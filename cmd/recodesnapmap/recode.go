package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	dcmgmt "github.com/vpngen/dc-mgmt"
	snapCrypto "github.com/vpngen/keydesk-snap/core/crypto"
	snapSnap "github.com/vpngen/keydesk-snap/core/snap"
	"github.com/vpngen/keydesk/keydesk/storage"
)

var ErrUnexpectedSnapsEnd = fmt.Errorf("unexpected snaps end")

func createMapping(data *dcmgmt.AggrSnaps, opts *opts) (map[string]string, error) {
	epsk, err := base64.StdEncoding.DecodeString(data.EncryptedPreSharedSecret)
	if err != nil {
		return nil, fmt.Errorf("decode epsk: %w", err)
	}

	psk, err := snapCrypto.DecryptSecret(opts.authPrivKey, epsk)
	if err != nil {
		return nil, fmt.Errorf("decrypt psk: %w", err)
	}

	plan, err := processSnap(data, opts, psk)
	if err != nil {
		return nil, fmt.Errorf("create non-mirrored plan: %w", err)
	}

	return plan, nil
}

func processSnap(data *dcmgmt.AggrSnaps, opts *opts, psk []byte) (map[string]string, error) {
	plan := make(map[string]string)

	for _, snap := range data.Snaps {
		brigade, err := decodeEncryptedBrigade(snap, psk, opts)
		if err != nil {
			return nil, fmt.Errorf("decode brigade: %w", err)
		}

		addr := brigade.EndpointIPv4.String()

		plan[snap.BrigadeID] = addr
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
