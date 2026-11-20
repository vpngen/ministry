package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	jwtsvc "github.com/vpngen/keydesk/pkg/jwt"
	sshVng "github.com/vpngen/ministry/internal/ssh"
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
	browserSessionSubject     = "miniapp-browser-session"
	browserSessionTTL         = 30 * time.Minute

	// Static Tribute deep links, matching admin-panel/flows/vip.json's
	// vip_get_urls stage - VIP payment already works through these today
	// (see PaymentProvider doc comment); POST /brigades just needs to
	// return the same links as data instead of a chat button.
	//
	// Tribute has two distinct link shapes for the same product: a
	// t.me/tribute/app deep link (opens Tribute's own Mini App inside
	// Telegram) and a web.tribute.tg one (for a caller with no Telegram
	// context at all). Both encode the same subscription, just the "s"
	// prefix on the slug differs. The frontend picks whichever matches its
	// own rendering context (state.mode) - the backend doesn't try to guess.
	defaultTributeMonthURL    = "https://t.me/tribute/app?startapp=sEtM"
	defaultTributeYearURL     = "https://t.me/tribute/app?startapp=sEtO"
	defaultTributeMonthURLWeb = "https://web.tribute.tg/s/EtM"
	defaultTributeYearURLWeb  = "https://web.tribute.tg/s/EtO"

	// No PRO-specific Tribute product exists yet (would come from Tribute's
	// dashboard) - deliberately no default here, unlike VIP above. An unset
	// env var means PRO checkout has no payment link yet; the frontend must
	// show that honestly rather than silently reusing VIP's links.
	defaultTributePROMonthURL    = ""
	defaultTributePROYearURL     = ""
	defaultTributePROMonthURLWeb = ""
	defaultTributePROYearURLWeb  = ""

	sshkeyDefaultPath    = "/etc/vgdept"
	sshkeyRemoteUsername = "_valera_" // matches cmd/createbrigade, cmd/checkbrigadier
)

type config struct {
	dbURL                   string
	listenAddr              string
	vipEndpoint             string
	obfsUUID                uuid.UUID
	jwtIssuer               jwtsvc.KeydeskTokenIssuer
	browserSessionIssuer    jwtsvc.KeydeskTokenIssuer
	browserSessionValidator jwtsvc.KeydeskTokenAuthorizer
	telegramBotToken        string
	tributeMonthURL         string
	tributeYearURL          string
	tributeMonthURLWeb      string
	tributeYearURLWeb       string
	tributePROMonthURL      string
	tributePROYearURL       string
	tributePROMonthURLWeb   string
	tributePROYearURLWeb    string
	sshKeyFilename          string
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

	signingMethod, jwtPrivkey, jwtPubkey, keyID, err := jwtsvc.ReadPrivateSSHKey(vipPrivkeyFn)
	if err != nil {
		return cfg, fmt.Errorf("read jwt private key: %w", err)
	}

	jwtopts := jwtsvc.KeydeskTokenOptions{
		Issuer:        "ministry",
		Subject:       reserveSubject,
		Audience:      []string{"ministry", "socket"},
		SigningMethod: signingMethod,
		VipURL:        vipEndpoint,
	}

	cfg.jwtIssuer = jwtsvc.NewKeydeskTokenIssuer(jwtPrivkey, keyID, jwtopts)

	// Separate issuer/validator pair for Mini App web-session tokens (POST
	// /session/browser): same signing key, distinct Subject/Audience so these
	// short-lived anonymous-browser tokens can never be confused with the
	// "reserve" tokens used for outbound partner-server calls above.
	browserOpts := jwtsvc.KeydeskTokenOptions{
		Issuer:        "ministry-api",
		Subject:       browserSessionSubject,
		Audience:      []string{"miniapp"},
		SigningMethod: signingMethod,
	}

	cfg.browserSessionIssuer = jwtsvc.NewKeydeskTokenIssuer(jwtPrivkey, keyID, browserOpts)
	cfg.browserSessionValidator = jwtsvc.NewKeydeskTokenAuthorizer(jwtPubkey, browserOpts)

	cfg.telegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if cfg.telegramBotToken == "" {
		fmt.Fprintln(os.Stderr, "WARNING: TELEGRAM_BOT_TOKEN not set - Mini App endpoints will reject all requests")
	}

	cfg.tributeMonthURL = os.Getenv("TRIBUTE_MONTH_URL")
	if cfg.tributeMonthURL == "" {
		cfg.tributeMonthURL = defaultTributeMonthURL
	}

	cfg.tributeYearURL = os.Getenv("TRIBUTE_YEAR_URL")
	if cfg.tributeYearURL == "" {
		cfg.tributeYearURL = defaultTributeYearURL
	}

	cfg.tributeMonthURLWeb = os.Getenv("TRIBUTE_MONTH_URL_WEB")
	if cfg.tributeMonthURLWeb == "" {
		cfg.tributeMonthURLWeb = defaultTributeMonthURLWeb
	}

	cfg.tributeYearURLWeb = os.Getenv("TRIBUTE_YEAR_URL_WEB")
	if cfg.tributeYearURLWeb == "" {
		cfg.tributeYearURLWeb = defaultTributeYearURLWeb
	}

	cfg.tributePROMonthURL = os.Getenv("TRIBUTE_PRO_MONTH_URL")
	if cfg.tributePROMonthURL == "" {
		cfg.tributePROMonthURL = defaultTributePROMonthURL
	}

	cfg.tributePROYearURL = os.Getenv("TRIBUTE_PRO_YEAR_URL")
	if cfg.tributePROYearURL == "" {
		cfg.tributePROYearURL = defaultTributePROYearURL
	}

	cfg.tributePROMonthURLWeb = os.Getenv("TRIBUTE_PRO_MONTH_URL_WEB")
	if cfg.tributePROMonthURLWeb == "" {
		cfg.tributePROMonthURLWeb = defaultTributePROMonthURLWeb
	}

	cfg.tributePROYearURLWeb = os.Getenv("TRIBUTE_PRO_YEAR_URL_WEB")
	if cfg.tributePROYearURLWeb == "" {
		cfg.tributePROYearURLWeb = defaultTributePROYearURLWeb
	}

	// SSH access to provision a brigade's actual keys (core.ComposeBrigade),
	// same capability cmd/createbrigade and cmd/checkbrigadier already have -
	// needed for POST /brigades' PRO path, which (unlike VIP) provisions
	// synchronously rather than waiting on the ckvip cron. Non-fatal if
	// unset: the rest of the service still works, PRO brigade creation just
	// errors until configured.
	sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: no SSH key configured (%s) - PRO brigade creation will fail until set\n", err)
	}

	cfg.sshKeyFilename = sshKeyFilename

	return cfg, nil
}
