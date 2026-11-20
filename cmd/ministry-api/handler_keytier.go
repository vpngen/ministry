package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const keyTierLogTag = "keyTierHandler"

// KeyInfo describes one brigade's key tier, as returned by GET /keys.
type KeyInfo struct {
	BrigadeID           uuid.UUID  `json:"brigade_id"`
	Tier                string     `json:"tier"`
	PendingDeactivation bool       `json:"pending_deactivation"`
	DeactivateAt        *time.Time `json:"deactivate_at,omitempty"`
}

// ListKeysResponse is returned by GET /keys.
type ListKeysResponse struct {
	Keys []KeyInfo `json:"keys"`
}

// UpgradeQuoteRequest is the body sent to POST /keys/{brigade_id}/upgrade/quote.
type UpgradeQuoteRequest struct {
	ToTier string `json:"to_tier"`
}

// UpgradeQuoteResponse is what the (future) checkout screen renders.
// Cost fields are stubbed until the billing system exists — see BillingQuoter.
type UpgradeQuoteResponse struct {
	ToTier          string    `json:"to_tier"`
	ProratedCost    Money     `json:"prorated_cost"`
	NextPeriodCost  Money     `json:"next_period_cost"`
	NextInvoiceDate time.Time `json:"next_invoice_date"`
}

// UpgradeConfirmRequest is the body sent to POST /keys/{brigade_id}/upgrade/confirm.
type UpgradeConfirmRequest struct {
	ToTier string `json:"to_tier"`
}

// UpgradeConfirmResponse reports the outcome of confirming an upgrade.
// SocketKeyID is stubbed until the Socket registry integration exists — see KeyIssuer.
type UpgradeConfirmResponse struct {
	Tier        string `json:"tier"`
	SocketKeyID string `json:"socket_key_id"`
}

// listKeysHandler returns the calling Mini App user's brigade(s) with their
// current key tier. Brigades with no brigadier_key_tier row yet are implicitly
// 'free' (that's the column default, applied lazily on first upgrade).
//
// @Summary      List the caller's keys
// @Description  Returns tier info for every brigade the authenticated Telegram user owns.
// @Tags         keytier
// @Produce      json
// @Param        Authorization  header    string  true  "tma <initData>"
// @Success      200            {object}  ListKeysResponse
// @Failure      401            {object}  ErrorResponse
// @Failure      500            {object}  ErrorResponse
// @Router       /keys [get]
func listKeysHandler(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := miniAppIdentityFromContext(r.Context())
		if !ok {
			log.Printf("%s: missing mini app identity", keyTierLogTag)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		keys, err := fetchKeyTiers(r.Context(), db, identity.BrigadeIDs)
		if err != nil {
			log.Printf("%s: fetch key tiers: %s", keyTierLogTag, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListKeysResponse{Keys: keys})
	}
}

func fetchKeyTiers(ctx context.Context, db *pgxpool.Pool, brigadeIDs []uuid.UUID) ([]KeyInfo, error) {
	keys := make([]KeyInfo, 0, len(brigadeIDs))
	for _, brigadeID := range brigadeIDs {
		info, err := fetchKeyTier(ctx, db, brigadeID)
		if err != nil {
			return nil, fmt.Errorf("query brigade %s: %w", brigadeID, err)
		}

		keys = append(keys, info)
	}

	return keys, nil
}

// fetchKeyTier reads one brigade's tier row, defaulting to 'free' when no
// row exists yet (the column default, applied lazily on first upgrade).
func fetchKeyTier(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) (KeyInfo, error) {
	sqlSelect := `
	SELECT tier, pending_deactivation, deactivate_at FROM %s WHERE brigade_id = $1
	`

	info := KeyInfo{BrigadeID: brigadeID, Tier: "free"}

	err := db.QueryRow(ctx, fmt.Sprintf(sqlSelect, pgx.Identifier{defaultBrigadesSchema, "brigadier_key_tier"}.Sanitize()), brigadeID).
		Scan(&info.Tier, &info.PendingDeactivation, &info.DeactivateAt)
	if err != nil && err != pgx.ErrNoRows {
		return KeyInfo{}, err
	}

	return info, nil
}

// upgradeQuoteHandler returns a checkout quote for upgrading a brigade's key.
// Cost fields come from BillingQuoter, which is stubbed until the billing
// system (built by another team, in parallel) is available.
//
// @Summary      Get an upgrade checkout quote
// @Tags         keytier
// @Accept       json
// @Produce      json
// @Param        brigade_id     path      string                true  "brigade id"
// @Param        Authorization  header    string                true  "tma <initData>"
// @Param        request        body      UpgradeQuoteRequest   true  "target tier"
// @Success      200            {object}  UpgradeQuoteResponse
// @Failure      400            {object}  ErrorResponse
// @Failure      401            {object}  ErrorResponse
// @Failure      403            {object}  ErrorResponse
// @Router       /keys/{brigade_id}/upgrade/quote [post]
func upgradeQuoteHandler(quoter BillingQuoter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		brigadeID, ok := authorizedBrigadeID(w, r)
		if !ok {
			return
		}

		var req UpgradeQuoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !isValidProTier(req.ToTier) {
			writeError(w, "to_tier must be pro", http.StatusBadRequest)
			return
		}

		proratedCost, nextPeriodCost, nextInvoiceDate, err := quoter.GetCheckoutQuote(r.Context(), brigadeID, req.ToTier)
		if err != nil {
			log.Printf("%s: get checkout quote: %s", keyTierLogTag, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UpgradeQuoteResponse{
			ToTier:          req.ToTier,
			ProratedCost:    proratedCost,
			NextPeriodCost:  nextPeriodCost,
			NextInvoiceDate: nextInvoiceDate,
		})
	}
}

// upgradeConfirmHandler issues a new key via KeyIssuer (stubbed until the
// Socket registry API is published) and records the upgrade request.
// billing_event_id is left unset in this phase — there is no real payment
// webhook yet to supply one; the unique index on it still protects against
// future redelivery once the billing integration lands.
//
// @Summary      Confirm a key upgrade
// @Tags         keytier
// @Accept       json
// @Produce      json
// @Param        brigade_id     path      string                  true  "brigade id"
// @Param        Authorization  header    string                  true  "tma <initData>"
// @Param        request        body      UpgradeConfirmRequest   true  "target tier"
// @Success      200            {object}  UpgradeConfirmResponse
// @Failure      400            {object}  ErrorResponse
// @Failure      401            {object}  ErrorResponse
// @Failure      403            {object}  ErrorResponse
// @Failure      409            {object}  ErrorResponse
// @Router       /keys/{brigade_id}/upgrade/confirm [post]
func upgradeConfirmHandler(db *pgxpool.Pool, issuer KeyIssuer) http.HandlerFunc {
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

		requestID, err := recordUpgradeRequested(r.Context(), db, brigadeID, req.ToTier)
		if err != nil {
			log.Printf("%s: record upgrade request for brigade %s: %s", keyTierLogTag, brigadeID, err)
			writeError(w, "an upgrade is already in progress for this brigade", http.StatusConflict)
			return
		}

		socketKeyID, err := issuer.IssueKey(r.Context(), brigadeID, req.ToTier)
		if err != nil {
			log.Printf("%s: issue key for brigade %s: %s", keyTierLogTag, brigadeID, err)
			markUpgradeFailed(r.Context(), db, requestID)
			writeError(w, "key issuance failed", http.StatusBadGateway)
			return
		}

		if err := markUpgradeKeyIssued(r.Context(), db, requestID, socketKeyID); err != nil {
			log.Printf("%s: mark upgrade key issued for brigade %s: %s", keyTierLogTag, brigadeID, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		if identity, ok := miniAppIdentityFromContext(r.Context()); ok && identity.SessionJTI != nil {
			deleteBrowserSession(r.Context(), db, *identity.SessionJTI)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UpgradeConfirmResponse{Tier: req.ToTier, SocketKeyID: socketKeyID})
	}
}

// authorizedBrigadeID reads {brigade_id} from the path and checks it's one
// of the authenticated caller's own brigades.
func authorizedBrigadeID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	identity, ok := miniAppIdentityFromContext(r.Context())
	if !ok {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return uuid.Nil, false
	}

	brigadeID, err := uuid.Parse(r.PathValue("brigade_id"))
	if err != nil {
		writeError(w, "invalid brigade_id", http.StatusBadRequest)
		return uuid.Nil, false
	}

	for _, id := range identity.BrigadeIDs {
		if id == brigadeID {
			return brigadeID, true
		}
	}

	writeError(w, "forbidden", http.StatusForbidden)
	return uuid.Nil, false
}

// isValidProTier checks the requested upgrade target. PRO is a single tier
// for this PoC - key-differentiated pro_basic/pro_unlim tiers are a real
// future feature, not built yet (see plan doc).
func isValidProTier(tier string) bool {
	return tier == "pro"
}

func recordUpgradeRequested(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID, toTier string) (uuid.UUID, error) {
	sqlInsert := `
	INSERT INTO %s (brigade_id, from_tier, to_tier, status)
	VALUES ($1, 'free', $2, 'requested')
	RETURNING id
	`

	var id uuid.UUID
	if err := db.QueryRow(ctx, fmt.Sprintf(sqlInsert, pgx.Identifier{defaultBrigadesSchema, "brigadier_key_upgrade_requests"}.Sanitize()), brigadeID, toTier).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("insert upgrade request: %w", err)
	}

	return id, nil
}

func markUpgradeKeyIssued(ctx context.Context, db *pgxpool.Pool, requestID uuid.UUID, socketKeyID string) error {
	sqlUpdate := `
	UPDATE %s SET status = 'completed', socket_key_id = $2 WHERE id = $1
	`

	if _, err := db.Exec(ctx, fmt.Sprintf(sqlUpdate, pgx.Identifier{defaultBrigadesSchema, "brigadier_key_upgrade_requests"}.Sanitize()), requestID, socketKeyID); err != nil {
		return fmt.Errorf("update upgrade request: %w", err)
	}

	return nil
}

func markUpgradeFailed(ctx context.Context, db *pgxpool.Pool, requestID uuid.UUID) {
	sqlUpdate := `
	UPDATE %s SET status = 'failed' WHERE id = $1
	`

	if _, err := db.Exec(ctx, fmt.Sprintf(sqlUpdate, pgx.Identifier{defaultBrigadesSchema, "brigadier_key_upgrade_requests"}.Sanitize()), requestID); err != nil {
		log.Printf("%s: mark upgrade request %s failed: %s", keyTierLogTag, requestID, err)
	}
}
