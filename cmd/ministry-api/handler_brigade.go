package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"

	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/wordsgens/namesgenerator"
)

const brigadeLogTag = "brigadeHandler"

const brigadeCreationType = "http_api" // distinguishes Mini App/web-originated brigades from ssh_api ones in audit trails

// keydeskURL is returned as-is by GET /brigades/{id}/keydesk-link. A
// brigade-scoped authenticated deep link (vs. this static entry point) is a
// worthwhile follow-up, but keydesk's actual URL-based auth contract for
// that isn't confirmed - see the project plan's note on this.
const keydeskURL = "https://vpn.works"

// CreateBrigadeRequest is the body sent to POST /brigades.
type CreateBrigadeRequest struct {
	Tier   string `json:"tier"`   // "vip" | "pro"
	Period string `json:"period"` // "month" | "year"
}

// CreateBrigadeResponse is returned by POST /brigades. CheckoutURL and
// CheckoutURLWeb are the two Tribute deep-link shapes for the same
// subscription (VIP always has both; PRO has them only once
// TRIBUTE_PRO_MONTH_URL/YEAR_URL[_WEB] are configured - empty until then,
// which the frontend must show honestly rather than falling back to VIP's
// link). The backend doesn't guess which one applies - the frontend picks
// based on its own state.mode (Telegram vs plain browser), since that's
// ground truth only the client has.
// CheckoutRef is PRO's internal tracking reference (stub PaymentProvider
// today, kept for a future real webhook match).
//
// SessionToken is always set: creating a brand new brigade must work with no
// prior identity (a first-time Telegram user has no existing brigades to
// resolve tma auth against; an anonymous web visitor has no browser session
// yet), so this response doubles as the caller's proof of ownership for
// this specific new brigade_id on subsequent checkout/grants/keydesk-link
// calls - same mechanism as /identify's session_token.
type CreateBrigadeResponse struct {
	BrigadeID      uuid.UUID `json:"brigade_id"`
	Status         string    `json:"status"`
	CheckoutURL    string    `json:"checkout_url,omitempty"`
	CheckoutURLWeb string    `json:"checkout_url_web,omitempty"`
	CheckoutRef    string    `json:"checkout_ref,omitempty"`
	SessionToken   string    `json:"session_token"`
}

// BrigadeGrantsResponse is returned by GET /brigades/{id}/grants.
type BrigadeGrantsResponse struct {
	Status     string `json:"status"` // "pending" | "ready"
	Name       string `json:"name,omitempty"`
	Mnemo      string `json:"mnemo,omitempty"`
	OutlineKey string `json:"outline_key,omitempty"`
	Proto0Key  string `json:"proto0_key,omitempty"`
}

// createBrigadeHandler reserves (VIP) or fully provisions (PRO) a brand new
// brigade. VIP reuses the exact same reservation path reqvipid already uses
// (core.RequestVIPBrigade) and the same static Tribute links already in
// admin-panel/flows/vip.json - payment confirmation then arrives the same
// way it does for the chat flow, via ckvip's fetchPaidUsers cron. PRO has no
// equivalent external pipeline, so it provisions synchronously here
// (core.CreateBrigade + core.ComposeBrigade, same as cmd/createbrigade) and
// is tagged with its tier via the KeyIssuer stub.
func createBrigadeHandler(db *pgxpool.Pool, cfg config, partnerID uuid.UUID, sshconf *ssh.ClientConfig, issuer KeyIssuer, provider PaymentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Unlike every other endpoint, missing/absent identity is not an
		// error here - see resolveOptionalIdentity's doc comment.
		identity := resolveOptionalIdentity(r, db, cfg)

		var req CreateBrigadeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		switch req.Tier {
		case "vip":
			createVIPBrigade(w, r, db, cfg, partnerID, identity, req)
		case "pro":
			createPROBrigade(w, r, db, cfg, sshconf, partnerID, provider, identity, req)
		default:
			writeError(w, "tier must be vip or pro", http.StatusBadRequest)
		}
	}
}

func createVIPBrigade(w http.ResponseWriter, r *http.Request, db *pgxpool.Pool, cfg config, partnerID uuid.UUID, identity miniAppIdentity, req CreateBrigadeRequest) {
	// RequestVIPBrigade requires a Telegram user id (it stores the
	// brigade<->telegram link the ckvip cron and partner-server pipeline
	// depend on) - there is no equivalent anchor for an anonymous web/browser
	// session yet, so VIP creation is Telegram Mini App only for now.
	if identity.TelegramID == 0 {
		writeError(w, "VIP brigade creation is only available from the Telegram Mini App", http.StatusNotImplemented)
		return
	}

	checkoutURL, checkoutURLWeb := cfg.tributeMonthURL, cfg.tributeMonthURLWeb
	if req.Period == "year" {
		checkoutURL, checkoutURLWeb = cfg.tributeYearURL, cfg.tributeYearURLWeb
	}

	labelID := uuid.New().String()

	brigadeID, err := core.RequestVIPBrigade(r.Context(), db, partnerID, brigadeCreationType, nil, "", "", labelID, time.Now().Unix(), identity.TelegramID, "ru")
	if err != nil {
		log.Printf("%s: request vip brigade: %s", brigadeLogTag, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	sessionToken, err := mintBrowserSession(r.Context(), db, cfg, brigadeID)
	if err != nil {
		log.Printf("%s: mint session for %s: %s", brigadeLogTag, brigadeID, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CreateBrigadeResponse{BrigadeID: brigadeID, Status: "reserved", CheckoutURL: checkoutURL, CheckoutURLWeb: checkoutURLWeb, SessionToken: sessionToken})
}

func createPROBrigade(w http.ResponseWriter, r *http.Request, db *pgxpool.Pool, cfg config, sshconf *ssh.ClientConfig, partnerID uuid.UUID, provider PaymentProvider, identity miniAppIdentity, req CreateBrigadeRequest) {
	if sshconf == nil {
		writeError(w, "brigade provisioning is not configured", http.StatusServiceUnavailable)
		return
	}

	labelID := uuid.New().String()

	brigadeID, mnemo, fullname, person, err := core.CreateBrigade(r.Context(), db, seedExtra, partnerID, brigadeCreationType, nil, "", "", labelID, time.Now().Unix())
	if err != nil {
		log.Printf("%s: create brigade: %s", brigadeLogTag, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Mirrors cmd/createbrigade's own behavior: link the brigade for future
	// tma-authenticated lookups (GET /keys etc.) when created from Telegram.
	// Web callers have no telegram_id; they rely solely on the session
	// token returned below.
	if identity.TelegramID != 0 {
		if err := upsertFreeTelegramID(r.Context(), db, defaultBrigadesSchema, brigadeID, identity.TelegramID, "ru"); err != nil {
			log.Printf("%s: upsert free_telegram_id for %s: %s", brigadeLogTag, brigadeID, err)
		}
	}

	vpnconf, err := core.ComposeBrigade(r.Context(), db, sshconf, brigadeLogTag, false, false, brigadeID, fullname, person)
	if err != nil {
		log.Printf("%s: compose brigade %s: %s", brigadeLogTag, brigadeID, err)
		writeError(w, "brigade provisioning failed", http.StatusBadGateway)
		return
	}

	// PRO provisions synchronously (unlike VIP, which arrives later via the
	// ckvip cron), but GET /brigades/{id}/grants reads head.vip_messages for
	// both paths - write the same shape ckvip's composeBrigade writes so
	// that one endpoint serves both.
	if err := persistBrigadeGrants(r.Context(), db, brigadeID, mnemo, fullname, person, vpnconf); err != nil {
		log.Printf("%s: persist grants for %s: %s", brigadeLogTag, brigadeID, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	checkoutRef, err := provider.CreateCheckout(r.Context(), brigadeID, req.Tier, req.Period)
	if err != nil {
		log.Printf("%s: create checkout for %s: %s", brigadeLogTag, brigadeID, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := recordUpgradeRequested(r.Context(), db, brigadeID, req.Tier); err != nil {
		log.Printf("%s: record upgrade request for %s: %s", brigadeLogTag, brigadeID, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	sessionToken, err := mintBrowserSession(r.Context(), db, cfg, brigadeID)
	if err != nil {
		log.Printf("%s: mint session for %s: %s", brigadeLogTag, brigadeID, err)
		writeError(w, "internal error", http.StatusInternalServerError)
		return
	}

	checkoutURL, checkoutURLWeb := cfg.tributePROMonthURL, cfg.tributePROMonthURLWeb
	if req.Period == "year" {
		checkoutURL, checkoutURLWeb = cfg.tributePROYearURL, cfg.tributePROYearURLWeb
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CreateBrigadeResponse{BrigadeID: brigadeID, Status: "checkout_pending", CheckoutURL: checkoutURL, CheckoutURLWeb: checkoutURLWeb, CheckoutRef: checkoutRef, SessionToken: sessionToken})
}

// confirmCheckoutHandler is the PRO-only payment confirmation step - stubbed
// (auto-confirms) until a real Tribute integration exists. VIP has no
// equivalent endpoint: its payment confirmation arrives asynchronously via
// the existing fetchPaidUsers/reserve pipeline, unchanged by this feature.
func confirmCheckoutHandler(db *pgxpool.Pool, issuer KeyIssuer, provider PaymentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		brigadeID, ok := authorizedBrigadeID(w, r)
		if !ok {
			return
		}

		var req UpgradeConfirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !isValidProTier(req.ToTier) {
			writeError(w, "to_tier must be pro", http.StatusBadRequest)
			return
		}

		checkoutRef, err := latestCheckoutRef(r.Context(), db, brigadeID)
		if err != nil {
			log.Printf("%s: latest checkout ref for %s: %s", brigadeLogTag, brigadeID, err)
			writeError(w, "no pending checkout for this brigade", http.StatusConflict)
			return
		}

		confirmed, err := provider.ConfirmPayment(r.Context(), checkoutRef)
		if err != nil || !confirmed {
			log.Printf("%s: confirm payment %s: err=%v confirmed=%v", brigadeLogTag, checkoutRef, err, confirmed)
			writeError(w, "payment not confirmed", http.StatusPaymentRequired)
			return
		}

		socketKeyID, err := issuer.IssueKey(r.Context(), brigadeID, req.ToTier)
		if err != nil {
			log.Printf("%s: issue key for %s: %s", brigadeLogTag, brigadeID, err)
			writeError(w, "key issuance failed", http.StatusBadGateway)
			return
		}

		if identity, ok := miniAppIdentityFromContext(r.Context()); ok && identity.SessionJTI != nil {
			deleteBrowserSession(r.Context(), db, *identity.SessionJTI)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UpgradeConfirmResponse{Tier: req.ToTier, SocketKeyID: socketKeyID})
	}
}

// persistBrigadeGrants writes a brigade's provisioned answer into
// head.vip_messages with finalizer=true, mirroring exactly what ckvip's
// composeBrigade writes for the VIP path (cmd/ckvip/newbrigade.go) - so
// GET /brigades/{id}/grants can read one table for both PRO (synchronous,
// written here) and VIP (async, written by the ckvip cron) brigades.
func persistBrigadeGrants(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID, mnemo, fullname string, person *namesgenerator.Person, vpnconf *dcmgmt.Answer) error {
	answer := ministry.Answer{
		Answer: dcmgmt.Answer{
			Answer: keydesk.Answer{
				Code:    http.StatusCreated,
				Desc:    http.StatusText(http.StatusCreated),
				Status:  keydesk.AnswerStatusSuccess,
				Configs: vpnconf.Answer.Configs,
			},
			KeydeskIPv6: vpnconf.KeydeskIPv6,
			FreeSlots:   vpnconf.FreeSlots,
		},
		Mnemo:  mnemo,
		Name:   fullname,
		Person: *person,
	}

	payload, err := json.Marshal(answer)
	if err != nil {
		return fmt.Errorf("marshal answer: %w", err)
	}

	sqlUpsert := `
	INSERT INTO %[1]s (brigade_id, mnemo, vpnconfig, finalizer, last_try)
	VALUES ($1, $2, $3, true, NOW() AT TIME ZONE 'UTC' - INTERVAL '1 HOUR')
	ON CONFLICT (brigade_id) DO UPDATE SET mnemo = $2, vpnconfig = $3, finalizer = true
	`

	if _, err := db.Exec(ctx, fmt.Sprintf(sqlUpsert, pgx.Identifier{defaultBrigadesSchema, "vip_messages"}.Sanitize()), brigadeID, mnemo, string(payload)); err != nil {
		return fmt.Errorf("upsert vip_messages: %w", err)
	}

	return nil
}

// upsertFreeTelegramID mirrors cmd/createbrigade's own helper of the same
// name/behavior.
func upsertFreeTelegramID(ctx context.Context, db *pgxpool.Pool, schema string, brigadeID uuid.UUID, telegramID int64, lang string) error {
	_, err := db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.free_telegram_ids (brigade_id, telegram_id, lang)
		VALUES ($1, $2, $3)
		ON CONFLICT (brigade_id) DO UPDATE SET telegram_id = EXCLUDED.telegram_id, lang = EXCLUDED.lang
	`, schema), brigadeID, telegramID, lang)

	return err
}

func latestCheckoutRef(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) (string, error) {
	// The stub PaymentProvider never persists its own checkout refs; reuse
	// the brigadier_key_upgrade_requests row createPROBrigade already wrote
	// as the source of truth for "there is a checkout in flight."
	sqlSelect := `
	SELECT id FROM %s WHERE brigade_id = $1 AND status NOT IN ('completed','failed','cancelled')
	ORDER BY created_time DESC LIMIT 1
	`

	var id uuid.UUID
	if err := db.QueryRow(ctx, fmt.Sprintf(sqlSelect, pgx.Identifier{defaultBrigadesSchema, "brigadier_key_upgrade_requests"}.Sanitize()), brigadeID).Scan(&id); err != nil {
		return "", fmt.Errorf("query open upgrade request: %w", err)
	}

	return id.String(), nil
}

// grantsHandler polls for a new brigade's provisioned credentials. For PRO
// (synchronous provisioning) this is ready on the very first poll; for VIP
// it mirrors msg_sync.go's polling against head.vip_messages, just reading
// the DB directly instead of over SSH readmsgs.
func grantsHandler(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		brigadeID, ok := authorizedBrigadeID(w, r)
		if !ok {
			return
		}

		grants, err := fetchBrigadeGrants(r.Context(), db, brigadeID)
		if err != nil {
			log.Printf("%s: fetch grants for %s: %s", brigadeLogTag, brigadeID, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(grants)
	}
}

func fetchBrigadeGrants(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) (BrigadeGrantsResponse, error) {
	sqlSelect := `SELECT finalizer, vpnconfig FROM %s WHERE brigade_id = $1`

	var (
		finalizer bool
		vpnconfig string
	)

	err := db.QueryRow(ctx, fmt.Sprintf(sqlSelect, pgx.Identifier{defaultBrigadesSchema, "vip_messages"}.Sanitize()), brigadeID).Scan(&finalizer, &vpnconfig)

	switch {
	case err == nil && finalizer:
		var answer ministry.VIPAnswer
		if uerr := json.Unmarshal([]byte(vpnconfig), &answer); uerr != nil {
			return BrigadeGrantsResponse{}, fmt.Errorf("unmarshal vpnconfig: %w", uerr)
		}

		grants := BrigadeGrantsResponse{Status: "ready", Name: answer.Name, Mnemo: answer.Mnemo}
		if answer.Configs.OutlineConfig != nil && answer.Configs.OutlineConfig.AccessKey != nil {
			grants.OutlineKey = *answer.Configs.OutlineConfig.AccessKey
		}
		if answer.Configs.Proto0Config != nil && answer.Configs.Proto0Config.AccessKey != nil {
			grants.Proto0Key = *answer.Configs.Proto0Config.AccessKey
		}

		return grants, nil
	case err == nil, errors.Is(err, pgx.ErrNoRows):
		return BrigadeGrantsResponse{Status: "pending"}, nil
	default:
		return BrigadeGrantsResponse{}, fmt.Errorf("query vip_messages: %w", err)
	}
}

// keydeskLinkHandler returns where to send the brigadier to manage their
// brigade. Static today - see the keydeskURL doc comment.
func keydeskLinkHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authorizedBrigadeID(w, r); !ok {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			URL string `json:"url"`
		}{URL: keydeskURL})
	}
}
