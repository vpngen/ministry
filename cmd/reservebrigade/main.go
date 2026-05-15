package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	jwtsvc "github.com/vpngen/keydesk/pkg/jwt"
	"github.com/vpngen/ministry/internal/pgsql"
)

const (
	defaultDatabaseURL        = "postgresql:///vgdept"
	defaultBrigadesSchema     = "head"
	defaultVipEndpoint        = "vip.vpn.works"
	reserveSubject            = "reserve"
	keydeskJwtDefaultDir      = "/etc/vgdept"
	keydeskJwtPrivkeyFileName = "keydesk-jwt.key"
	etcSubdir                 = "vg-keydesk"
)

// vipReservePayload is posted to the VIP server's partner_api/reserve endpoint.
type vipReservePayload struct {
	UserID       uuid.UUID `json:"user_id"`
	UserIdentity string    `json:"user_idenity"` // intentional typo: matches VIP server API
}

type vipReserveResponse struct {
	Result        string  `json:"result"`
	ExecutionTime float64 `json:"execution_time"`
}

type reserveResponse struct {
	OK bool `json:"ok"`
}

var errInvalidArgs = errors.New("invalid args")

func main() {
	var w io.WriteCloser

	token, brigadeID, userIdentity, chunked, jout, mock, err := parseArgs()
	if err != nil {
		log.Fatalf("%s: Can't parse args: %s\n", LogTag, err)
	}

	switch chunked {
	case true:
		w = httputil.NewChunkedWriter(os.Stdout)
		defer w.Close()
	default:
		w = os.Stdout
	}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	schema := os.Getenv("BRIGADES_ADMIN_SCHEMA")
	if schema == "" {
		schema = defaultBrigadesSchema
	}

	vipEndpoint := os.Getenv("VIP_ENDPOINT")
	if vipEndpoint == "" {
		vipEndpoint = defaultVipEndpoint
	}

	db, err := pgsql.CreateDBPool(dbURL)
	if err != nil {
		fatal(w, jout, "%s: Can't create db pool: %s\n", LogTag, err)
	}

	ctx := context.Background()

	_, ok, err := checkToken(ctx, db, schema, token)
	if err != nil || !ok {
		if err != nil {
			fatal(w, jout, "%s: Can't check token: %s\n", LogTag, err)
		}

		fatal(w, jout, "%s: Access denied\n", LogTag)
	}

	if !mock {
		jwtIssuer, err := loadJWTIssuer(vipEndpoint)
		if err != nil {
			fatal(w, jout, "%s: Can't load JWT issuer: %s\n", LogTag, err)
		}

		c := &http.Client{
			Timeout:   30 * time.Second,
			Transport: NewBearerAuthTransport(jwtIssuer, nil),
		}

		payload, err := json.Marshal(vipReservePayload{
			UserID:       brigadeID,
			UserIdentity: userIdentity,
		})
		if err != nil {
			fatal(w, jout, "%s: Can't marshal payload: %s\n", LogTag, err)
		}

		vipURL := fmt.Sprintf("https://%s/partner_api/reserve", vipEndpoint)

		vipReq, err := http.NewRequestWithContext(ctx, http.MethodPost, vipURL, bytes.NewReader(payload))
		if err != nil {
			fatal(w, jout, "%s: Can't build VIP request: %s\n", LogTag, err)
		}

		vipReq.Header.Set("Content-Type", "application/json")

		resp, err := c.Do(vipReq)
		if err != nil {
			fatal(w, jout, "%s: VIP server unreachable: %s\n", LogTag, err)
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fatal(w, jout, "%s: Can't read VIP response: %s\n", LogTag, err)
		}

		if resp.StatusCode != http.StatusOK {
			fatal(w, jout, "%s: VIP server error: %s\n", LogTag, body)
		}

		var vipResp vipReserveResponse
		if err := json.Unmarshal(body, &vipResp); err != nil {
			fatal(w, jout, "%s: Can't parse VIP response: %s\n", LogTag, err)
		}

		if vipResp.Result != "success" {
			fatal(w, jout, "%s: VIP server result: %s\n", LogTag, vipResp.Result)
		}
	}

	switch jout {
	case true:
		payload, err := json.Marshal(reserveResponse{OK: true})
		if err != nil {
			fatal(w, jout, "%s: Can't marshal answer: %s\n", LogTag, err)
		}

		if _, err := w.Write(payload); err != nil {
			fatal(w, jout, "%s: Can't write answer: %s\n", LogTag, err)
		}
	default:
		if _, err := fmt.Fprintln(w, "OK"); err != nil {
			log.Fatalf("%s: Can't print OK: %s\n", LogTag, err)
		}
	}
}

func loadJWTIssuer(vipEndpoint string) (*jwtsvc.KeydeskTokenIssuer, error) {
	vipPrivkeyFn := filepath.Join(keydeskJwtDefaultDir, keydeskJwtPrivkeyFileName)
	if _, err := os.Stat(vipPrivkeyFn); err != nil {
		sysUser, err := user.Current()
		if err == nil {
			vipPrivkeyFn = filepath.Join(sysUser.HomeDir, keydeskJwtPrivkeyFileName)
		}

		if _, err := os.Stat(vipPrivkeyFn); err != nil {
			p, err := os.Executable()
			if err != nil {
				return nil, fmt.Errorf("get executable path: %w", err)
			}

			vipPrivkeyFn = filepath.Join(filepath.Dir(p), etcSubdir, keydeskJwtPrivkeyFileName)
			if _, err := os.Stat(vipPrivkeyFn); err != nil {
				return nil, fmt.Errorf("stat jwt privkey %s: %w", vipPrivkeyFn, err)
			}
		}
	}

	signingMethod, jwtPrivkey, _, keyID, err := jwtsvc.ReadPrivateSSHKey(vipPrivkeyFn)
	if err != nil {
		return nil, fmt.Errorf("read jwt private key: %w", err)
	}

	opts := jwtsvc.KeydeskTokenOptions{
		Issuer:        "ministry",
		Subject:       reserveSubject,
		Audience:      []string{"ministry", "socket"},
		SigningMethod:  signingMethod,
		VipURL:        vipEndpoint,
	}

	issuer := jwtsvc.NewKeydeskTokenIssuer(jwtPrivkey, keyID, opts)

	return &issuer, nil
}

func parseArgs() ([]byte, uuid.UUID, string, bool, bool, bool, error) {
	chunked := flag.Bool("ch", false, "chunked output")
	jout := flag.Bool("j", false, "json output")
	mock := flag.Bool("mock", false, "mock mode")

	flag.Parse()

	if flag.NArg() != 3 {
		return nil, uuid.Nil, "", false, false, false, fmt.Errorf("args: %w", errInvalidArgs)
	}

	tokenRaw := flag.Arg(0)
	token := make([]byte, base64.URLEncoding.WithPadding(base64.NoPadding).DecodedLen(len(tokenRaw)))

	n, err := base64.URLEncoding.WithPadding(base64.NoPadding).Decode(token, []byte(tokenRaw))
	if err != nil {
		return nil, uuid.Nil, "", false, false, false, fmt.Errorf("token: %w", err)
	}

	token = token[:n]

	brigadeID, err := uuid.Parse(flag.Arg(1))
	if err != nil {
		return nil, uuid.Nil, "", false, false, false, fmt.Errorf("brigade_id: %w", err)
	}

	userIdentity := flag.Arg(2)
	if userIdentity == "" {
		return nil, uuid.Nil, "", false, false, false, fmt.Errorf("user_identity: %w", errInvalidArgs)
	}

	return token, brigadeID, userIdentity, *chunked, *jout, *mock, nil
}
