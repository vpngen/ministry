package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/vpngen/keydesk/keydesk"
	jwtsvc "github.com/vpngen/keydesk/pkg/jwt"
	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	sshkeyDefaultPath         = "/etc/vgdept"
	sshkeyRemoteUsername      = "_valera_"
	keydeskJwtPrivkeyFileName = "keydesk-jwt.key"
	etcSubdir                 = "vg-keydesk"
	defaultVipEndpoint        = "vip.vpn.works"
	listCommand               = "list_keys"
	defaultDatabaseURL        = "postgresql:///vgdept"
)

type config struct {
	jwtKeydeskIssuer jwtsvc.KeydeskTokenIssuer

	dbURL    string
	sshKeyFn string

	vipEndpoint string
	obfsUUID    uuid.UUID
}

func parseArgs() (config, error) {
	cfg := config{}

	vipEndpoint := os.Getenv("VIP_ENDPOINT")
	if vipEndpoint == "" {
		vipEndpoint = defaultVipEndpoint
	}

	cfg.vipEndpoint = vipEndpoint

	obfsKey := os.Getenv("OBFS_UUID")

	obfsUUID, err := uuid.Parse(obfsKey)
	if err != nil {
		return cfg, fmt.Errorf("parse obfs uuid: %w", err)
	}

	obfsUUID[6] &= 0x0F
	obfsUUID[8] &= 0x3F

	cfg.obfsUUID = obfsUUID

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	cfg.dbURL = dbURL

	sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
	if err != nil {
		return cfg, fmt.Errorf("lookup for ssh key: %w", err)
	}

	cfg.sshKeyFn = sshKeyFilename

	etcDir := flag.String("c", "", "Dir for config files (for test). Default: "+keydesk.DefaultEtcDir)

	flag.Parse()

	edir := ""
	if *etcDir != "" {
		dir, err := filepath.Abs(*etcDir)
		if err != nil {
			return cfg, fmt.Errorf("etc dir: %w", err)
		}

		edir = filepath.Join(dir, etcSubdir)
	}

	vipPrivkeyFn := filepath.Join(edir, keydeskJwtPrivkeyFileName)
	if _, err := os.Stat(vipPrivkeyFn); err != nil {
		return cfg, fmt.Errorf("stat jwt privkey %s: %w", vipPrivkeyFn, err)
	}

	signingMethod, jwtKeydeskPrivkey, _, keyId, err := jwtsvc.ReadPrivateSSHKey(vipPrivkeyFn)
	if err != nil {
		return cfg, fmt.Errorf("read jwt vip private key: %w", err)
	}

	jwtopts := jwtsvc.KeydeskTokenOptions{
		Issuer:        "ministry",
		Subject:       listCommand,
		Audience:      []string{"ministry"},
		SigningMethod: signingMethod,
		VipURL:        vipEndpoint,
	}

	jwtopts.Audience = append(jwtopts.Audience, "socket")
	cfg.jwtKeydeskIssuer = jwtsvc.NewKeydeskTokenIssuer(jwtKeydeskPrivkey, keyId, jwtopts)

	return cfg, nil
}
