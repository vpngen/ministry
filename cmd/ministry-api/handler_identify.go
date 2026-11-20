package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vpngen/ministry/internal/core"
)

const identifyLogTag = "identifyHandler"

// IdentifyRequest is the body sent to POST /identify.
type IdentifyRequest struct {
	Name  string `json:"name"`
	Words string `json:"words"`
}

// IdentifyResponse is returned on a successful match. KeyCount is a
// simplification: the schema has no per-key concept yet (see
// keytier_iface.go), so it's the VIP-purchased key count for VIP brigades
// (brigadier_vip.vip_users) or 1 (the single base key/mnemo) otherwise.
//
// SessionToken is a browser-scoped session (mintBrowserSession, valid
// browserSessionTTL) for the web flow's subsequent GET /keys and
// POST /keys/{id}/upgrade/* calls. It is minted here, not via a separate
// endpoint, so that only a caller who just proved name+6-words for this
// brigade can ever obtain a session scoped to it - a bare brigade_id handed
// to a standalone "start session" endpoint would let anyone mint a session
// for any brigade they can guess/know the UUID of. Telegram Mini App callers
// ignore this field; they already have tma auth per-request.
type IdentifyResponse struct {
	BrigadeID    uuid.UUID `json:"brigade_id"`
	Tier         string    `json:"tier"`
	KeyCount     int       `json:"key_count"`
	SessionToken string    `json:"session_token"`
}

// identifyHandler is the ministry-api equivalent of embassy-tgbot's chat-only
// "identify by name+6-words" flow (steps_vip_identify.go -> CheckBrigadierID
// -> SSH checkbrigadier). It calls core.CheckBrigadier directly against the
// DB pool ministry-api already holds, with no SSH round trip.
//
// @Summary      Identify an existing brigade by name + 6 words
// @Tags         identify
// @Accept       json
// @Produce      json
// @Param        request  body      IdentifyRequest  true  "brigadier name + 6 mnemonic words"
// @Success      200      {object}  IdentifyResponse
// @Failure      400      {object}  ErrorResponse
// @Failure      404      {object}  ErrorResponse
// @Failure      429      {object}  ErrorResponse
// @Router       /identify [post]
func identifyHandler(db *pgxpool.Pool, cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := clientIPFromRequest(r)

		locked, retryAfter, err := checkIdentifyLockout(r.Context(), db, clientIP)
		if err != nil {
			log.Printf("%s: check lockout for %s: %s", identifyLogTag, clientIP, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if locked {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
			writeError(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}

		var req IdentifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		name := normalizeIdentifyText(req.Name)
		words := normalizeIdentifyText(req.Words)
		if name == "" || len(strings.Fields(words)) < 6 {
			writeError(w, "name and 6 words are required", http.StatusBadRequest)
			return
		}

		brigadeID, _, deleted, _, _, _, _, _, err := core.CheckBrigadier(r.Context(), db, seedExtra, name, words, false)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if lerr := recordIdentifyFailure(r.Context(), db, clientIP); lerr != nil {
					log.Printf("%s: record failure for %s: %s", identifyLogTag, clientIP, lerr)
				}
				writeError(w, "brigade not found", http.StatusNotFound)
				return
			}

			log.Printf("%s: check brigadier: %s", identifyLogTag, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		if deleted {
			if lerr := recordIdentifyFailure(r.Context(), db, clientIP); lerr != nil {
				log.Printf("%s: record failure for %s: %s", identifyLogTag, clientIP, lerr)
			}
			writeError(w, "brigade not found", http.StatusNotFound)
			return
		}

		tier, keyCount, err := fetchTierAndKeyCount(r.Context(), db, brigadeID)
		if err != nil {
			log.Printf("%s: fetch tier for %s: %s", identifyLogTag, brigadeID, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		sessionToken, err := mintBrowserSession(r.Context(), db, cfg, brigadeID)
		if err != nil {
			log.Printf("%s: mint browser session for %s: %s", identifyLogTag, brigadeID, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IdentifyResponse{BrigadeID: brigadeID, Tier: tier, KeyCount: keyCount, SessionToken: sessionToken})
	}
}

func normalizeIdentifyText(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, ",", " ")), " ")
}

func fetchTierAndKeyCount(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) (string, int, error) {
	info, err := fetchKeyTier(ctx, db, brigadeID)
	if err != nil {
		return "", 0, err
	}
	tier := info.Tier

	sqlVIPUsers := `SELECT vip_users FROM %s WHERE brigade_id = $1`

	var vipUsers int
	err = db.QueryRow(ctx, fmt.Sprintf(sqlVIPUsers, pgx.Identifier{defaultBrigadesSchema, "brigadier_vip"}.Sanitize()), brigadeID).Scan(&vipUsers)

	switch {
	case err == nil:
		return "vip", vipUsers, nil
	case err == pgx.ErrNoRows:
		return tier, 1, nil
	default:
		return "", 0, fmt.Errorf("query vip_users: %w", err)
	}
}

// identifyLockDurationForCount mirrors embassy-tgbot's
// vipIdentifyLockDurationForCount: 1st fail -> 1h, 2nd -> 12h, 3rd+ -> 24h.
func identifyLockDurationForCount(count int) time.Duration {
	switch {
	case count <= 1:
		return time.Hour
	case count == 2:
		return 12 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func checkIdentifyLockout(ctx context.Context, db *pgxpool.Pool, clientIP string) (bool, time.Duration, error) {
	sqlSelect := `SELECT locked_until FROM %s WHERE client_ip = $1`

	var lockedUntil *time.Time
	err := db.QueryRow(ctx, fmt.Sprintf(sqlSelect, pgx.Identifier{defaultBrigadesSchema, "identify_attempts"}.Sanitize()), clientIP).Scan(&lockedUntil)

	switch {
	case err == nil:
		if lockedUntil == nil {
			return false, 0, nil
		}
		remaining := time.Until(*lockedUntil)
		if remaining <= 0 {
			return false, 0, nil
		}
		return true, remaining, nil
	case err == pgx.ErrNoRows:
		return false, 0, nil
	default:
		return false, 0, fmt.Errorf("query lockout: %w", err)
	}
}

// recordIdentifyFailure bumps fail_count for clientIP and (re)arms the
// lockout with the escalated duration, mirroring embassy-tgbot's
// recordVIPIdentifyFailure. Two statements (bump, then lock) since the new
// lock duration depends on the post-bump count.
func recordIdentifyFailure(ctx context.Context, db *pgxpool.Pool, clientIP string) error {
	table := pgx.Identifier{defaultBrigadesSchema, "identify_attempts"}.Sanitize()

	var failCount int
	sqlBump := fmt.Sprintf(`
	INSERT INTO %[1]s (client_ip, fail_count)
	VALUES ($1, 1)
	ON CONFLICT (client_ip) DO UPDATE SET fail_count = %[1]s.fail_count + 1
	RETURNING fail_count
	`, table)

	if err := db.QueryRow(ctx, sqlBump, clientIP).Scan(&failCount); err != nil {
		return fmt.Errorf("bump fail_count: %w", err)
	}

	lockedUntil := time.Now().Add(identifyLockDurationForCount(failCount))

	sqlLock := fmt.Sprintf(`UPDATE %s SET locked_until = $2 WHERE client_ip = $1`, table)
	if _, err := db.Exec(ctx, sqlLock, clientIP, lockedUntil); err != nil {
		return fmt.Errorf("set locked_until: %w", err)
	}

	return nil
}

func clientIPFromRequest(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip := strings.TrimSpace(strings.Split(fwd, ",")[0]); ip != "" {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
