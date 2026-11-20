package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
	httpSwagger "github.com/swaggo/http-swagger"
	"golang.org/x/crypto/ssh"

	"github.com/vpngen/ministry/internal/pgsql"
	sshVng "github.com/vpngen/ministry/internal/ssh"

	_ "github.com/vpngen/ministry/cmd/ministry-api/docs" // generated swagger docs
)

// @title           Ministry API
// @version         1.0
// @description     Ministry REST API — proxy layer over VIP server (vip.vpn.works).
// @basePath        /
// @schemes         http https

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse config: %s\n", err)
		os.Exit(1)
	}

	db, err := pgsql.CreateDBPool(cfg.dbURL)
	if err != nil {
		log.Fatalf("Can't create db pool: %s\n", err)
	}

	issuer := &stubKeyIssuer{db: db, schema: defaultBrigadesSchema}
	quoter := &stubBillingQuoter{}
	provider := &stubPaymentProvider{}

	// Resolves ministry-api's own partner identity for brigades it creates
	// on the Mini App/web app's behalf (core.RequestVIPBrigade/CreateBrigade
	// both require one, same as reqvipid/createbrigade do via their own
	// -j token argument). Non-fatal: /brigades just errors until configured.
	var partnerID uuid.UUID
	if tok := os.Getenv("MINISTRY_API_PARTNER_TOKEN"); tok != "" {
		tokenBytes, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(tok)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: MINISTRY_API_PARTNER_TOKEN is not valid base64url: %s\n", err)
		} else if id, ok, err := checkToken(context.Background(), db, defaultBrigadesSchema, tokenBytes); err != nil || !ok {
			fmt.Fprintf(os.Stderr, "WARNING: MINISTRY_API_PARTNER_TOKEN not recognized - POST /brigades will fail until fixed\n")
		} else {
			partnerID = id
		}
	} else {
		fmt.Fprintln(os.Stderr, "WARNING: MINISTRY_API_PARTNER_TOKEN not set - POST /brigades will fail until configured")
	}

	var sshconf *ssh.ClientConfig
	if cfg.sshKeyFilename != "" {
		sc, err := sshVng.CreateSSHConfig(cfg.sshKeyFilename, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: build ssh config: %s - PRO brigade creation will fail until fixed\n", err)
		} else {
			sshconf = sc
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /reserve", reserveHandler(db, cfg))
	mux.HandleFunc("GET /keys", anyAuthMiddleware(db, cfg, listKeysHandler(db)))
	mux.HandleFunc("POST /keys/{brigade_id}/upgrade/quote", anyAuthMiddleware(db, cfg, upgradeQuoteHandler(quoter)))
	mux.HandleFunc("POST /keys/{brigade_id}/upgrade/confirm", anyAuthMiddleware(db, cfg, upgradeConfirmHandler(db, issuer)))
	mux.HandleFunc("POST /identify", identifyHandler(db, cfg))
	// createBrigadeHandler resolves identity itself (leniently - see
	// resolveOptionalIdentity) since creating a brand new brigade must also
	// work for a caller with no prior identity at all.
	mux.HandleFunc("POST /brigades", createBrigadeHandler(db, cfg, partnerID, sshconf, issuer, provider))
	mux.HandleFunc("POST /brigades/{brigade_id}/checkout/confirm", anyAuthMiddleware(db, cfg, confirmCheckoutHandler(db, issuer, provider)))
	mux.HandleFunc("GET /brigades/{brigade_id}/grants", anyAuthMiddleware(db, cfg, grantsHandler(db)))
	mux.HandleFunc("GET /brigades/{brigade_id}/keydesk-link", anyAuthMiddleware(db, cfg, keydeskLinkHandler()))
	mux.HandleFunc("/api/documentation/", httpSwagger.WrapHandler)

	go runBrowserSessionCleanup(db)

	log.Printf("Listening on %s\n", cfg.listenAddr)

	if err := http.ListenAndServe(cfg.listenAddr, mux); err != nil {
		log.Fatalf("ListenAndServe: %s\n", err)
	}
}
