package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	jwtsvc "github.com/vpngen/keydesk/pkg/jwt"
	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	sshkeyDefaultPath    = "/etc/vgdept"
	keydeskJwtDefaultDir = "/etc/vgdept"

	sshkeyRemoteUsername      = "_valera_"
	keydeskJwtPrivkeyFileName = "keydesk-jwt.key"
	etcSubdir                 = "vg-keydesk"
	defaultVipEndpoint        = "vip.vpn.works"
	listCommand               = "list_keys"
	defaultDatabaseURL        = "postgresql:///vgdept"
)

type config struct {
	debug     bool
	onlyfetch bool
	silent    bool
	mock      bool

	jwtKeydeskIssuer jwtsvc.KeydeskTokenIssuer

	dbURL    string
	sshKeyFn string

	vipEndpoint string
	obfsUUID    uuid.UUID

	// mockTelegramIDs is the fixed set of stage test accounts that should
	// always be treated as VIP-paid, regardless of environment. Configured
	// via MOCK_TELEGRAM_IDS instead of hardcoded so prod can simply leave it
	// unset - an empty set here means no telegram_id is ever a test account.
	mockTelegramIDs map[int64]struct{}
}

func parseMockTelegramIDs(raw string) (map[int64]struct{}, error) {
	ids := make(map[int64]struct{})

	if raw == "" {
		return ids, nil
	}

	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse telegram id %q: %w", s, err)
		}

		ids[id] = struct{}{}
	}

	return ids, nil
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

	cfg.mock = os.Getenv("MOCK") == "true"

	mockTelegramIDs, err := parseMockTelegramIDs(os.Getenv("MOCK_TELEGRAM_IDS"))
	if err != nil {
		return cfg, fmt.Errorf("parse mock telegram ids: %w", err)
	}

	cfg.mockTelegramIDs = mockTelegramIDs

	if !cfg.mock {
		sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
		if err != nil {
			return cfg, fmt.Errorf("lookup for ssh key: %w", err)
		}

		cfg.sshKeyFn = sshKeyFilename
	}

	debug := flag.Bool("debug", false, "Debug")
	silent := flag.Bool("s", false, "Silent")
	onlyfetch := flag.Bool("onlyfetch", false, "Only fetch and print answers")

	flag.Parse()

	cfg.debug = *debug
	cfg.onlyfetch = *onlyfetch
	cfg.silent = *silent && !*debug

	sysUser, err := user.Current()
	if err != nil {
		return cfg, fmt.Errorf("user: %w", err)
	}

	vipPrivkeyFn := filepath.Join(keydeskJwtDefaultDir, keydeskJwtPrivkeyFileName)
	if _, err := os.Stat(vipPrivkeyFn); err != nil {
		vipPrivkeyFn = filepath.Join(sysUser.HomeDir, keydeskJwtPrivkeyFileName)
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
