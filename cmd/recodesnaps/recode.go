package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strings"

	dcmgmt "github.com/vpngen/dc-mgmt"
	snapCrypto "github.com/vpngen/keydesk-snap/core/crypto"
	snapSnap "github.com/vpngen/keydesk-snap/core/snap"
	"github.com/vpngen/keydesk/keydesk/storage"
	"github.com/vpngen/vpngine/naclkey"
	"golang.org/x/crypto/nacl/box"
)

var ErrUnexpectedSnapsEnd = fmt.Errorf("unexpected snaps end")

func createRestorePlan(data *dcmgmt.AggrSnaps, reservConfig *dcmgmt.ReservationConfig, opts *opts) (*dcmgmt.RestorePlan, error) {
	epsk, err := base64.StdEncoding.DecodeString(data.EncryptedPreSharedSecret)
	if err != nil {
		return nil, fmt.Errorf("decode epsk: %w", err)
	}

	psk, err := snapCrypto.DecryptSecret(opts.authPrivKey, epsk)
	if err != nil {
		return nil, fmt.Errorf("decrypt psk: %w", err)
	}

	switch opts.mirror {
	case true:
		plan, err := createRestorePlanMirrored(data, reservConfig, opts, psk)
		if err != nil {
			return nil, fmt.Errorf("create mirrored plan: %w", err)
		}

		return plan, nil
	default:
		plan, err := createRestorePlanNotMirrored(data, reservConfig, opts, psk)
		if err != nil {
			return nil, fmt.Errorf("create non-mirrored plan: %w", err)
		}

		return plan, nil
	}
}

type prepNode struct {
	config *dcmgmt.RestoreNodeConfig
	pubkey [naclkey.NaclBoxKeyLength]byte
	used   map[string]struct{}
}

var (
	ErrMirroredIPNotFound   = fmt.Errorf("mirrored ip not found")
	ErrMirroredIPDuplicated = fmt.Errorf("mirrored ip duplicated")
)

func createRestorePlanMirrored(data *dcmgmt.AggrSnaps, reservConfig *dcmgmt.ReservationConfig,
	opts *opts, psk []byte,
) (*dcmgmt.RestorePlan, error) {
	nodes := make(map[string]*prepNode)

	for _, snap := range data.Snaps {
		brigade, err := decodeEncryptedBrigade(snap, psk, opts)
		if err != nil {
			return nil, fmt.Errorf("decode brigade: %w", err)
		}

		addr := brigade.EndpointIPv4.String()

		var (
			node *prepNode
			ok   bool
		)

		for _, ctrl := range reservConfig.Plan {
			for _, slot := range ctrl.Slots {
				if slot == addr {
					node, ok = nodes[ctrl.ControlIP]
					if !ok {
						routerPub, err := naclkey.UnmarshalPublicKey([]byte(ctrl.RouterNACLPubKey))
						if err != nil {
							return nil, fmt.Errorf("unmarshal router public key: %w", err)
						}

						nodeConfig := &dcmgmt.RestoreNodeConfig{
							ControlIP: ctrl.ControlIP,
							Snaps:     make([]dcmgmt.PreparedSnap, 0, len(ctrl.Slots)),
						}

						node = &prepNode{
							config: nodeConfig,
							pubkey: routerPub,
							used:   make(map[string]struct{}),
						}

						nodes[ctrl.ControlIP] = node
					}
				}
			}
		}

		if node == nil {
			return nil, fmt.Errorf("%w: %s", ErrMirroredIPNotFound, addr)
		}

		if _, ok := node.used[addr]; ok {
			return nil, fmt.Errorf("%w: %s: %s", ErrMirroredIPDuplicated, addr, brigade.BrigadeID)
		}

		snap, err := encodeEncryptedBrigade(brigade, addr, reservConfig.ReservationID, &node.pubkey, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "encode brigade: %s\n", err)

			continue
			// return nil, fmt.Errorf("encode brigade: %w", err)
		}

		node.config.Snaps = append(node.config.Snaps, *snap)

		node.used[addr] = struct{}{}
	}

	plan := &dcmgmt.RestorePlan{
		ReservationID: reservConfig.ReservationID,
		RealmFP:       opts.targetRealmFP,
	}

	for _, node := range nodes {
		plan.Plan = append(plan.Plan, *node.config)
	}

	return plan, nil
}

func createRestorePlanNotMirrored(data *dcmgmt.AggrSnaps, reservConfig *dcmgmt.ReservationConfig,
	opts *opts, psk []byte,
) (*dcmgmt.RestorePlan, error) {
	plan := &dcmgmt.RestorePlan{
		ReservationID: reservConfig.ReservationID,
		RealmFP:       opts.targetRealmFP,
	}

	for _, ctrl := range reservConfig.Plan {
		routerPub, err := naclkey.UnmarshalPublicKey([]byte(ctrl.RouterNACLPubKey))
		if err != nil {
			return nil, fmt.Errorf("unmarshal router public key: %w", err)
		}

		nodeConfig := &dcmgmt.RestoreNodeConfig{
			ControlIP: ctrl.ControlIP,
			Snaps:     make([]dcmgmt.PreparedSnap, 0, len(ctrl.Slots)),
		}

		for _, slot := range ctrl.Slots {
			brigade, err := decodeEncryptedBrigade(data.Snaps[0], psk, opts)
			if err != nil {
				return nil, fmt.Errorf("decode brigade: %w", err)
			}

			snap, err := encodeEncryptedBrigade(brigade, slot, reservConfig.ReservationID, &routerPub, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "encode brigade: %s\n", err)

				continue
				// return nil, fmt.Errorf("encode brigade: %w", err)
			}

			data.Snaps = data.Snaps[1:]

			nodeConfig.Snaps = append(nodeConfig.Snaps, *snap)
		}

		plan.Plan = append(plan.Plan, *nodeConfig)
	}

	return plan, nil
}

func decodeEncryptedBrigade(snap *dcmgmt.EncryptedBrigade,
	psk []byte, opts *opts,
) (*storage.Brigade, error) {
	if snap.RealmKeyFP != "" {
		return nil, fmt.Errorf("%w: non-empty realm key fingerprint", ErrInvalidSnapshotData)
	}

	if snap.AuthorityKeyFP == "" {
		return nil, fmt.Errorf("%w: empty authority key fingerprint", ErrInvalidSnapshotData)
	}

	if opts.authFP != snap.AuthorityKeyFP {
		return nil, fmt.Errorf("authority: %w: %s != %s", ErrKeysMismatch,
			opts.authFP, snap.AuthorityKeyFP)
	}

	elocker, err := base64.StdEncoding.DecodeString(snap.EncryptedLockerSecret)
	if err != nil {
		return nil, fmt.Errorf("decode elocker: %w", err)
	}

	locker, err := snapCrypto.DecryptSecret(opts.authPrivKey, elocker)
	if err != nil {
		return nil, fmt.Errorf("decrypt elocker: %w", err)
	}

	esecret64, ok := snap.Secrets[opts.authFP]
	if !ok {
		return nil, fmt.Errorf("%w: no secret for authority: %s", ErrInvalidSnapshotData, opts.authFP)
	}

	esecret, err := base64.StdEncoding.DecodeString(esecret64)
	if err != nil {
		return nil, fmt.Errorf("decode esecret: %w", err)
	}

	secret, err := snapCrypto.DecryptSecret(opts.authPrivKey, esecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}

	finalSecret := make([]byte, 0, len([]byte(snap.Tag))+len([]byte(snap.BrigadeID))+8+8+len(psk)+len(locker)+len(secret))
	finalSecret = fmt.Append(finalSecret, snap.Tag, snap.BrigadeID, snap.GlobalSnapAt.Unix(), snap.LocalSnapAt.Unix(), psk, locker, secret)

	brigade, err := decodeSnap(snap.Payload, snap.BrigadeID, finalSecret)
	if err != nil {
		return nil, fmt.Errorf("prepare snap: %w", err)
	}

	return brigade, nil
}

func encodeEncryptedBrigade(brigade *storage.Brigade,
	slot string, reservationID string, routerPub *[naclkey.NaclBoxKeyLength]byte,
	opts *opts,
) (*dcmgmt.PreparedSnap, error) {
	if err := cleanBrigade(brigade); err != nil {
		return nil, fmt.Errorf("clear snap: %w", err)
	}

	if err := recodeBrigade(brigade, slot, routerPub, opts); err != nil {
		return nil, fmt.Errorf("recode snap: %w", err)
	}

	esec, epayload, err := encryptBrigade(brigade, reservationID, opts)
	if err != nil {
		return nil, fmt.Errorf("encrypt brigade: %w", err)
	}

	return &dcmgmt.PreparedSnap{
		BrigadeID:       brigade.BrigadeID,
		EndpointIPv4:    slot,
		DomainNames:     getDomains(brigade),
		EncryptedSecret: esec,
		Payload:         epayload,
	}, nil
}

func getDomains(brigade *storage.Brigade) []string {
	return []string{brigade.EndpointDomain}
}

func decodeSnap(payload string, brigadeID string, secret []byte) (*storage.Brigade, error) {
	dec := base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))

	data, err := snapSnap.DecryptDecompressSnapshot(dec, secret)
	if err != nil {
		return nil, fmt.Errorf("decrypt decompress snapshot: %w", err)
	}

	// fmt.Printf("%s\n", data)

	var brigade storage.Brigade

	if err := json.Unmarshal(data, &brigade); err != nil {
		return nil, fmt.Errorf("unmarshal brigade: %w", err)
	}

	if brigade.BrigadeID != brigadeID {
		return nil, fmt.Errorf("%w: %s != %s", ErrInvalidSnapshotData, brigade.BrigadeID, brigadeID)
	}

	return &brigade, nil
}

func cleanBrigade(brigade *storage.Brigade) error {
	brigade.BrigadeCounters = storage.BrigadeCounters{}
	brigade.StatsCountersStack = storage.StatsCountersStack{}
	brigade.Endpoints = storage.UsersNetworks{}

	for _, user := range brigade.Users {
		user.Quotas = storage.Quota{Ver: user.Quotas.Ver}
	}

	return nil
}

func recodeBrigade(brigade *storage.Brigade, slot string, routerPub *[naclkey.NaclBoxKeyLength]byte, opts *opts) error {
	ep, err := netip.ParseAddr(slot)
	if err != nil {
		return fmt.Errorf("parse addr: %w", err)
	}

	brigade.EndpointIPv4 = ep

	wgPrivateRouterEnc, err := reEncodeNACL(brigade.WgPrivateShufflerEnc, routerPub, opts.masterPrivKey)
	if err != nil {
		return fmt.Errorf("re-encode wg private: %w: %s", err, brigade.BrigadeID)
	}

	brigade.WgPrivateRouterEnc = wgPrivateRouterEnc

	if brigade.OvCAKeyShufflerEnc != "" {
		ovCAKeyShufflerEnc, err := base64.StdEncoding.DecodeString(brigade.OvCAKeyShufflerEnc)
		if err != nil {
			return fmt.Errorf("decode ovca key: %w", err)
		}

		ovCAKeyRouterEnc, err := reEncodeNACL(ovCAKeyShufflerEnc, routerPub, opts.masterPrivKey)
		if err != nil {
			return fmt.Errorf("re-encode ovca key: %w", err)
		}

		brigade.OvCAKeyRouterEnc = base64.StdEncoding.EncodeToString(ovCAKeyRouterEnc)
	}

	if brigade.IPSecPSKShufflerEnc != "" {
		ipSecPSKShufflerEnc, err := base64.StdEncoding.DecodeString(brigade.IPSecPSKShufflerEnc)
		if err != nil {
			return fmt.Errorf("decode ipsec psk: %w", err)
		}

		ipSecPSKRouterEnc, err := reEncodeNACL(ipSecPSKShufflerEnc, routerPub, opts.masterPrivKey)
		if err != nil {
			return fmt.Errorf("re-encode ipsec psk: %w", err)
		}

		brigade.IPSecPSKRouterEnc = base64.StdEncoding.EncodeToString(ipSecPSKRouterEnc)
	}

	for _, user := range brigade.Users {
		if err := recodeUser(user, routerPub, opts); err != nil {
			return fmt.Errorf("recode user: %w", err)
		}
	}

	return nil
}

func recodeUser(user *storage.User, routerPub *[naclkey.NaclBoxKeyLength]byte, opts *opts) error {
	wgPSKShufflerEnc, err := reEncodeNACL(user.WgPSKShufflerEnc, routerPub, opts.masterPrivKey)
	if err != nil {
		return fmt.Errorf("re-encode wg psk: %w", err)
	}

	user.WgPSKRouterEnc = wgPSKShufflerEnc

	if user.CloakByPassUIDShufflerEnc != "" {
		cloakByPassUIDShufflerEnc, err := base64.StdEncoding.DecodeString(user.CloakByPassUIDShufflerEnc)
		if err != nil {
			return fmt.Errorf("decode cloak bypass uid: %w", err)
		}

		cloakByPassUIDRouterEnc, err := reEncodeNACL(cloakByPassUIDShufflerEnc, routerPub, opts.masterPrivKey)
		if err != nil {
			return fmt.Errorf("re-encode cloak bypass uid: %w", err)
		}

		user.CloakByPassUIDRouterEnc = base64.StdEncoding.EncodeToString(cloakByPassUIDRouterEnc)
	}

	if user.IPSecUsernameShufflerEnc != "" {
		ipSecUsernameShufflerEnc, err := base64.StdEncoding.DecodeString(user.IPSecUsernameShufflerEnc)
		if err != nil {
			return fmt.Errorf("decode ipsec username: %w", err)
		}

		ipSecUsernameRouterEnc, err := reEncodeNACL(ipSecUsernameShufflerEnc, routerPub, opts.masterPrivKey)
		if err != nil {
			return fmt.Errorf("re-encode ipsec username: %w", err)
		}

		user.IPSecUsernameRouterEnc = base64.StdEncoding.EncodeToString(ipSecUsernameRouterEnc)
	}

	if user.IPSecPasswordShufflerEnc != "" {
		ipSecPasswordShufflerEnc, err := base64.StdEncoding.DecodeString(user.IPSecPasswordShufflerEnc)
		if err != nil {
			return fmt.Errorf("decode ipsec password: %w", err)
		}

		ipSecPasswordRouterEnc, err := reEncodeNACL(ipSecPasswordShufflerEnc, routerPub, opts.masterPrivKey)
		if err != nil {
			return fmt.Errorf("re-encode ipsec password: %w", err)
		}

		user.IPSecPasswordRouterEnc = base64.StdEncoding.EncodeToString(ipSecPasswordRouterEnc)
	}

	if user.OutlineSecretShufflerEnc != "" {
		outlineSecretShufflerEnc, err := base64.StdEncoding.DecodeString(user.OutlineSecretShufflerEnc)
		if err != nil {
			return fmt.Errorf("decode outline secret: %w", err)
		}

		outlineSecretRouterEnc, err := reEncodeNACL(outlineSecretShufflerEnc, routerPub, opts.masterPrivKey)
		if err != nil {
			return fmt.Errorf("re-encode outline secret: %w", err)
		}

		user.OutlineSecretRouterEnc = base64.StdEncoding.EncodeToString(outlineSecretRouterEnc)
	}

	return nil
}

var ErrDecryptionFailed = fmt.Errorf("decryption failed")

func reEncodeNACL(payload []byte, routerPub *[naclkey.NaclBoxKeyLength]byte, masterPriv naclkey.NaclBoxKeypair) ([]byte, error) {
	decrypted, ok := box.OpenAnonymous(nil, payload, &masterPriv.Public, &masterPriv.Private)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	// re-encode
	routerReEncrypted, err := box.SealAnonymous(nil, decrypted, routerPub, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("router seal: %w", err)
	}

	return routerReEncrypted, nil
}

func encryptBrigade(brigade *storage.Brigade, reservationID string, opts *opts) (string, string, error) {
	secret, err := snapCrypto.GenSecret(snapSnap.SecretSize)
	if err != nil {
		return "", "", fmt.Errorf("gen secret: %w", err)
	}

	esecret, err := snapCrypto.EncryptSecret(opts.targetPubKey, secret)
	if err != nil {
		return "", "", fmt.Errorf("encrypt secret: %w", err)
	}

	payload, err := json.Marshal(brigade)
	if err != nil {
		return "", "", fmt.Errorf("marshal brigade: %w", err)
	}

	finalsecret := make([]byte, 0, len([]byte(brigade.BrigadeID))+len([]byte(reservationID))+len(secret))
	finalsecret = fmt.Append(finalsecret, brigade.BrigadeID, reservationID, secret)

	epayload, err := snapSnap.CompressEncryptSnapshot(bytes.NewBuffer(payload), finalsecret)
	if err != nil {
		return "", "", fmt.Errorf("compress encrypt snapshot: %w", err)
	}

	return base64.StdEncoding.EncodeToString(esecret), base64.StdEncoding.EncodeToString(epayload), nil
}
