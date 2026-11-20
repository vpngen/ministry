package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	miniAppLogTag         = "miniAppAuth"
	miniAppAuthScheme     = "tma"
	miniAppInitDataMaxAge = 24 * time.Hour
)

var (
	ErrMiniAppUnauthorized = errors.New("mini app: unauthorized")
	ErrMiniAppStaleAuth    = errors.New("mini app: stale auth_date")
	ErrMiniAppNoBrigades   = errors.New("mini app: no brigade found for telegram id")
)

// miniAppTelegramUser is the subset of Telegram's WebAppUser we need.
type miniAppTelegramUser struct {
	ID int64 `json:"id"`
}

// verifyInitData implements Telegram's documented Mini App initData check:
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func verifyInitData(initData, botToken string) (miniAppTelegramUser, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return miniAppTelegramUser{}, fmt.Errorf("parse init data: %w", err)
	}

	receivedHash := values.Get("hash")
	if receivedHash == "" {
		return miniAppTelegramUser{}, ErrMiniAppUnauthorized
	}
	values.Del("hash")

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+values.Get(k))
	}
	dataCheckString := strings.Join(pairs, "\n")

	secretKey := hmac.New(sha256.New, []byte("WebAppData"))
	secretKey.Write([]byte(botToken))

	mac := hmac.New(sha256.New, secretKey.Sum(nil))
	mac.Write([]byte(dataCheckString))
	computedHash := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(computedHash), []byte(receivedHash)) {
		return miniAppTelegramUser{}, ErrMiniAppUnauthorized
	}

	authDateRaw := values.Get("auth_date")
	authDateUnix, err := strconv.ParseInt(authDateRaw, 10, 64)
	if err != nil {
		return miniAppTelegramUser{}, fmt.Errorf("parse auth_date: %w", err)
	}

	if time.Since(time.Unix(authDateUnix, 0)) > miniAppInitDataMaxAge {
		return miniAppTelegramUser{}, ErrMiniAppStaleAuth
	}

	var user miniAppTelegramUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil {
		return miniAppTelegramUser{}, fmt.Errorf("parse user: %w", err)
	}

	return user, nil
}

// resolveBrigadeIDs maps a Telegram user id to the brigade(s) it identifies,
// via free_telegram_ids (every brigade is enrolled here at creation time)
// falling back to vip_telegram_ids for brigades only ever touched through
// the VIP flow.
func resolveBrigadeIDs(ctx context.Context, db *pgxpool.Pool, schema string, telegramID int64) ([]uuid.UUID, error) {
	sqlLookup := `
	SELECT brigade_id FROM %s WHERE telegram_id = $1
	UNION
	SELECT brigade_id FROM %s WHERE telegram_id = $1
	`

	rows, err := db.Query(ctx, fmt.Sprintf(sqlLookup,
		pgx.Identifier{schema, "free_telegram_ids"}.Sanitize(),
		pgx.Identifier{schema, "vip_telegram_ids"}.Sanitize(),
	), telegramID)
	if err != nil {
		return nil, fmt.Errorf("query brigades: %w", err)
	}
	defer rows.Close()

	var brigadeIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan brigade id: %w", err)
		}
		brigadeIDs = append(brigadeIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	if len(brigadeIDs) == 0 {
		return nil, ErrMiniAppNoBrigades
	}

	return brigadeIDs, nil
}

type miniAppContextKey struct{}

// miniAppIdentity is injected into the request context by miniAppAuthMiddleware.
// SessionJTI is set only when auth came via a Bearer browser session (nil for
// tma) - it lets a completing handler delete its own browser_sessions row
// once the purchase/upgrade it was scoped to is done (see deleteBrowserSession).
type miniAppIdentity struct {
	TelegramID int64
	BrigadeIDs []uuid.UUID
	SessionJTI *uuid.UUID
}

func miniAppIdentityFromContext(ctx context.Context) (miniAppIdentity, bool) {
	id, ok := ctx.Value(miniAppContextKey{}).(miniAppIdentity)
	return id, ok
}

// miniAppAuthMiddleware validates the `Authorization: tma <initData>` header
// (Telegram's Mini App auth convention) and resolves it to the caller's
// brigade(s). This is a distinct trust model from the partner-bearer
// checkToken auth in token.go and is intentionally not merged with it.
func miniAppAuthMiddleware(db *pgxpool.Pool, cfg config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		scheme, initData, ok := strings.Cut(authHeader, " ")
		if !ok || scheme != miniAppAuthScheme {
			log.Printf("%s: missing or malformed Authorization header", miniAppLogTag)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := verifyInitData(initData, cfg.telegramBotToken)
		if err != nil {
			log.Printf("%s: init data verification failed: %s", miniAppLogTag, err)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		brigadeIDs, err := resolveBrigadeIDs(r.Context(), db, defaultBrigadesSchema, user.ID)
		if err != nil {
			log.Printf("%s: resolve brigades for telegram_id=%d: %s", miniAppLogTag, user.ID, err)
			writeError(w, "no brigades found", http.StatusNotFound)
			return
		}

		ctx := context.WithValue(r.Context(), miniAppContextKey{}, miniAppIdentity{
			TelegramID: user.ID,
			BrigadeIDs: brigadeIDs,
		})

		next(w, r.WithContext(ctx))
	}
}

// anyAuthMiddleware accepts either Telegram Mini App auth (Authorization:
// tma <initData>) or a web browser session (Authorization: Bearer <token>),
// dispatching on the header's scheme so GET /keys and POST /keys/*/upgrade/*
// can serve both the Telegram Mini App and the standalone web app from the
// same handlers and routes.
func anyAuthMiddleware(db *pgxpool.Pool, cfg config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme, _, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok {
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch scheme {
		case miniAppAuthScheme:
			miniAppAuthMiddleware(db, cfg, next)(w, r)
		case "Bearer":
			webSessionAuthMiddleware(db, cfg, next)(w, r)
		default:
			writeError(w, "unauthorized", http.StatusUnauthorized)
		}
	}
}

// mintBrowserSession issues a short-lived, brigade-scoped session token for
// the web (non-Telegram) flow, called right after a successful /identify.
// KeydeskTokenClaims (github.com/vpngen/keydesk pkg/jwt) has no free-form
// subject field to carry brigade_id, so the binding is stored server-side
// keyed by the token's jti (head.browser_sessions) instead of in the token.
func mintBrowserSession(ctx context.Context, db *pgxpool.Pool, cfg config, brigadeID uuid.UUID) (string, error) {
	claims := cfg.browserSessionIssuer.CreateToken(browserSessionTTL, false)

	jti, err := uuid.Parse(claims.ID)
	if err != nil {
		return "", fmt.Errorf("parse jti: %w", err)
	}

	sqlInsert := `INSERT INTO %s (jti, brigade_id, expires_at) VALUES ($1, $2, $3)`
	if _, err := db.Exec(ctx, fmt.Sprintf(sqlInsert, pgx.Identifier{defaultBrigadesSchema, "browser_sessions"}.Sanitize()),
		jti, brigadeID, claims.ExpiresAt.Time); err != nil {
		return "", fmt.Errorf("insert browser session: %w", err)
	}

	token, err := cfg.browserSessionIssuer.Sign(claims)
	if err != nil {
		return "", fmt.Errorf("sign browser session: %w", err)
	}

	return token, nil
}

// webSessionAuthMiddleware validates the `Authorization: Bearer <token>`
// session minted by mintBrowserSession and resolves it to the single brigade
// it was scoped to at /identify time. It populates the same miniAppIdentity
// context struct as miniAppAuthMiddleware so handler_keytier.go's handlers
// work unchanged regardless of which auth scheme the caller used.
func webSessionAuthMiddleware(db *pgxpool.Pool, cfg config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		scheme, token, ok := strings.Cut(authHeader, " ")
		if !ok || scheme != "Bearer" {
			log.Printf("%s: missing or malformed Authorization header", miniAppLogTag)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := cfg.browserSessionValidator.Validate(token)
		if err != nil || claims.Subject != browserSessionSubject {
			log.Printf("%s: browser session validation failed: %v", miniAppLogTag, err)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		jti, err := uuid.Parse(claims.ID)
		if err != nil {
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		brigadeID, err := resolveBrowserSessionBrigade(r.Context(), db, jti)
		if err != nil {
			log.Printf("%s: resolve browser session %s: %s", miniAppLogTag, jti, err)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), miniAppContextKey{}, miniAppIdentity{
			BrigadeIDs: []uuid.UUID{brigadeID},
			SessionJTI: &jti,
		})

		next(w, r.WithContext(ctx))
	}
}

// resolveOptionalIdentity is the lenient counterpart to
// miniAppAuthMiddleware/webSessionAuthMiddleware, used only by POST
// /brigades. Creating a brand new brigade must work for a caller with no
// prior identity at all (a first-time Telegram user has no existing
// brigades yet; an anonymous web visitor has no browser session yet) - so,
// unlike the other endpoints, missing/absent auth is not an error here, it
// just yields a zero-value identity. A present tma/Bearer header is still
// cryptographically verified when given; this never trusts an unverified
// caller-supplied identity. VIP creation requires a real TelegramID
// (checked by the caller, createVIPBrigade); PRO does not.
func resolveOptionalIdentity(r *http.Request, db *pgxpool.Pool, cfg config) miniAppIdentity {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok {
		return miniAppIdentity{}
	}

	switch scheme {
	case miniAppAuthScheme:
		user, err := verifyInitData(value, cfg.telegramBotToken)
		if err != nil {
			return miniAppIdentity{}
		}

		// Unlike miniAppAuthMiddleware, having no existing brigades yet is
		// the expected case here, not an error.
		brigadeIDs, err := resolveBrigadeIDs(r.Context(), db, defaultBrigadesSchema, user.ID)
		if err != nil && !errors.Is(err, ErrMiniAppNoBrigades) {
			log.Printf("%s: resolve brigades for telegram_id=%d: %s", miniAppLogTag, user.ID, err)
		}

		return miniAppIdentity{TelegramID: user.ID, BrigadeIDs: brigadeIDs}
	case "Bearer":
		claims, err := cfg.browserSessionValidator.Validate(value)
		if err != nil || claims.Subject != browserSessionSubject {
			return miniAppIdentity{}
		}

		jti, err := uuid.Parse(claims.ID)
		if err != nil {
			return miniAppIdentity{}
		}

		brigadeID, err := resolveBrowserSessionBrigade(r.Context(), db, jti)
		if err != nil {
			return miniAppIdentity{}
		}

		return miniAppIdentity{BrigadeIDs: []uuid.UUID{brigadeID}, SessionJTI: &jti}
	default:
		return miniAppIdentity{}
	}
}

// deleteBrowserSession removes a completed browser_sessions row - the
// "delete on completion" half of the temporary-session cleanup requirement
// (runBrowserSessionCleanup handles the "delete on TTL expiry" half for
// sessions that are abandoned before completing).
func deleteBrowserSession(ctx context.Context, db *pgxpool.Pool, jti uuid.UUID) {
	sqlDelete := `DELETE FROM %s WHERE jti = $1`
	if _, err := db.Exec(ctx, fmt.Sprintf(sqlDelete, pgx.Identifier{defaultBrigadesSchema, "browser_sessions"}.Sanitize()), jti); err != nil {
		log.Printf("%s: delete browser session %s: %s", miniAppLogTag, jti, err)
	}
}

// browserSessionCleanupInterval how often runBrowserSessionCleanup sweeps
// for abandoned (never-completed, TTL-expired) sessions.
const browserSessionCleanupInterval = 15 * time.Minute

// runBrowserSessionCleanup periodically deletes expired browser_sessions
// rows - a purchase/upgrade flow a user simply abandons never calls
// deleteBrowserSession itself, so this catches those once their TTL passes.
// Meant to run as its own goroutine for the lifetime of the process.
func runBrowserSessionCleanup(db *pgxpool.Pool) {
	ticker := time.NewTicker(browserSessionCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		sqlDelete := `DELETE FROM %s WHERE expires_at < (NOW() AT TIME ZONE 'UTC')`
		if _, err := db.Exec(context.Background(), fmt.Sprintf(sqlDelete, pgx.Identifier{defaultBrigadesSchema, "browser_sessions"}.Sanitize())); err != nil {
			log.Printf("%s: cleanup expired browser sessions: %s", miniAppLogTag, err)
		}
	}
}

func resolveBrowserSessionBrigade(ctx context.Context, db *pgxpool.Pool, jti uuid.UUID) (uuid.UUID, error) {
	sqlSelect := `SELECT brigade_id FROM %s WHERE jti = $1 AND expires_at > (NOW() AT TIME ZONE 'UTC')`

	var brigadeID uuid.UUID
	if err := db.QueryRow(ctx, fmt.Sprintf(sqlSelect, pgx.Identifier{defaultBrigadesSchema, "browser_sessions"}.Sanitize()), jti).Scan(&brigadeID); err != nil {
		return uuid.Nil, fmt.Errorf("query browser session: %w", err)
	}

	return brigadeID, nil
}
