package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/google/uuid"
	jwtsvc "github.com/vpngen/keydesk/pkg/jwt"
)

const (
	keydeskJwtDefaultDir      = "/etc/vgdept"
	keydeskJwtPrivkeyFileName = "keydesk-jwt.key"
	defaultListenAddr         = ":8080"
	defaultBrigadesSchema     = "head"
	etcSubdir                 = "vg-keydesk"
	defaultVipEndpoint        = "vip.vpn.works"
	defaultDatabaseURL        = "postgresql:///vgdept"
	reserveSubject            = "reserve"
)

type config struct {
	dbURL       string
	listenAddr  string
	vipEndpoint string
	obfsUUID    uuid.UUID
	jwtIssuer   jwtsvc.KeydeskTokenIssuer
}

func parseConfig() (config, error) {
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

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	cfg.listenAddr = listenAddr

	vipPrivkeyFn := filepath.Join(keydeskJwtDefaultDir, keydeskJwtPrivkeyFileName)
	if _, err := os.Stat(vipPrivkeyFn); err != nil {
		sysUser, err := user.Current()
		if err == nil {
			vipPrivkeyFn = filepath.Join(sysUser.HomeDir, keydeskJwtPrivkeyFileName)
		}

		if _, err := os.Stat(vipPrivkeyFn); err != nil {
			p, err := os.Executable()
			if err != nil {
				return cfg, fmt.Errorf("get executable path: %w", err)
			}

			vipPrivkeyFn = filepath.Join(filepath.Dir(p), etcSubdir, keydeskJwtPrivkeyFileName)
			if _, err := os.Stat(vipPrivkeyFn); err != nil {
				return cfg, fmt.Errorf("stat jwt privkey %s: %w", vipPrivkeyFn, err)
			}
		}
	}

	signingMethod, jwtPrivkey, _, keyID, err := jwtsvc.ReadPrivateSSHKey(vipPrivkeyFn)
	if err != nil {
		return cfg, fmt.Errorf("read jwt private key: %w", err)
	}

	jwtopts := jwtsvc.KeydeskTokenOptions{
		Issuer:        "ministry",
		Subject:       reserveSubject,
		Audience:      []string{"ministry", "socket"},
		SigningMethod:  signingMethod,
		VipURL:        vipEndpoint,
	}

	cfg.jwtIssuer = jwtsvc.NewKeydeskTokenIssuer(jwtPrivkey, keyID, jwtopts)

	return cfg, nil
}
