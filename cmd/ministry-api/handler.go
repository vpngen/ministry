package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultVIPDuration = 30 * 24 * time.Hour // 30 days

const logTag = "reserveHandler"

// ReserveRequest is the body sent by tgbot.
// brigade_id is the obfuscated UUID returned by reqvipid.
// user_identity is the Telegram ChatID.
// expire_at is optional — defaults to now+30 days if zero.
type ReserveRequest struct {
	BrigadeID    uuid.UUID `json:"brigade_id"`
	UserIdentity string    `json:"user_identity"`
	ExpireAt     time.Time `json:"expire_at,omitempty"`
}

// ReserveResponse is returned on success.
type ReserveResponse struct {
	OK bool `json:"ok"`
}

// ErrorResponse is returned on any error.
type ErrorResponse struct {
	Error string `json:"error"`
}

// vipReserveResponse is what partner_api/reserve returns on success.
type vipReserveResponse struct {
	Result        string  `json:"result"`
	ExecutionTime float64 `json:"execution_time"`
}

// vipReservePayload is what we POST to partner_api/reserve on the VIP server.
type vipReservePayload struct {
	UserID             uuid.UUID `json:"user_id"`
	UserIdentity       string    `json:"user_idenity"`        // note: typo is intentional, matches Oleg's API
	GoodExpiryDatetime time.Time `json:"good_expiry_datetime"` // required — VIP filters by this in fetchPaidUsers
}

// reserveHandler proxies a brigade reservation to the VIP server.
//
// @Summary      Reserve VIP brigade
// @Description  Marks a brigade as paid on the VIP server. The brigade must already
// @Description  exist in the ministry DB (created via SSH reqvipid).
// @Tags         vip
// @Accept       json
// @Produce      json
// @Param        Authorization  header    string          true  "Bearer <base64url-partner-token>"
// @Param        request        body      ReserveRequest  true  "brigade_id (obfuscated) + expire_at"
// @Success      200            {object}  ReserveResponse
// @Failure      400            {object}  ErrorResponse
// @Failure      401            {object}  ErrorResponse
// @Failure      502            {object}  ErrorResponse
// @Failure      500            {object}  ErrorResponse
// @Router       /reserve [post]
func reserveHandler(db *pgxpool.Pool, cfg config) http.HandlerFunc {
	c := &http.Client{
		Timeout:   30 * time.Second,
		Transport: NewBearerAuthTransport(&cfg.jwtIssuer, nil),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		log.Printf("%s: incoming request from %s", logTag, r.RemoteAddr)

		// --- auth ---
		authHeader := r.Header.Get("Authorization")
		parts := strings.SplitN(authHeader, " ", 2)

		if len(parts) != 2 || parts[0] != "Bearer" {
			log.Printf("%s: missing or malformed Authorization header", logTag)
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		tokenBytes, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(parts[1])
		if err != nil {
			log.Printf("%s: invalid token encoding: %s", logTag, err)
			writeError(w, "invalid token encoding", http.StatusUnauthorized)
			return
		}

		partnerID, ok, err := checkToken(ctx, db, defaultBrigadesSchema, tokenBytes)
		if err != nil || !ok {
			log.Printf("%s: token check failed: err=%v ok=%v", logTag, err, ok)
			writeError(w, "access denied", http.StatusUnauthorized)
			return
		}

		log.Printf("%s: authenticated partner %s", logTag, partnerID)

		// --- parse body ---
		var req ReserveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("%s: invalid request body: %s", logTag, err)
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.BrigadeID == uuid.Nil {
			log.Printf("%s: brigade_id is nil", logTag)
			writeError(w, "brigade_id required", http.StatusBadRequest)
			return
		}

		if req.UserIdentity == "" {
			log.Printf("%s: user_identity is empty", logTag)
			writeError(w, "user_identity required", http.StatusBadRequest)
			return
		}

		expireAt := req.ExpireAt
		if expireAt.IsZero() {
			expireAt = time.Now().UTC().Add(defaultVIPDuration)
		}

		log.Printf("%s: reserving brigade_id=%s user_identity=%s expire_at=%s", logTag, req.BrigadeID, req.UserIdentity, expireAt.Format(time.RFC3339))

		// --- call VIP server ---
		// brigade_id from tgbot is already obfuscated — that's the user_id the VIP server expects.
		payload, err := json.Marshal(vipReservePayload{
			UserID:             req.BrigadeID,
			UserIdentity:       req.UserIdentity,
			GoodExpiryDatetime: expireAt,
		})
		if err != nil {
			log.Printf("%s: marshal payload: %s", logTag, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		vipURL := fmt.Sprintf("https://%s/partner_api/reserve", cfg.vipEndpoint)
		log.Printf("%s: calling VIP server at %s", logTag, vipURL)

		vipReq, err := http.NewRequestWithContext(ctx, http.MethodPost, vipURL, bytes.NewReader(payload))
		if err != nil {
			log.Printf("%s: build vip request: %s", logTag, err)
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		vipReq.Header.Set("Content-Type", "application/json")

		resp, err := c.Do(vipReq)
		log.Printf("%s: JWT token used: %s", logTag, c.Transport.(*BearerAuthTransport).Token())
		if err != nil {
			log.Printf("%s: vip server unreachable: %s", logTag, err)
			writeError(w, fmt.Sprintf("vip server unreachable: %s", err), http.StatusBadGateway)
			return
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("%s: read vip response body: %s", logTag, err)
			writeError(w, "failed to read vip server response", http.StatusBadGateway)
			return
		}

		log.Printf("%s: vip server status=%d body=%s", logTag, resp.StatusCode, body)

		if resp.StatusCode != http.StatusOK {
			writeError(w, fmt.Sprintf("vip server: %s", body), http.StatusBadGateway)
			return
		}

		var vipResp vipReserveResponse
		if err := json.Unmarshal(body, &vipResp); err != nil {
			log.Printf("%s: parse vip response: %s", logTag, err)
			writeError(w, "invalid response from vip server", http.StatusBadGateway)
			return
		}

		if vipResp.Result != "success" {
			log.Printf("%s: vip server returned non-success result: %s", logTag, vipResp.Result)
			writeError(w, fmt.Sprintf("vip server: %s", vipResp.Result), http.StatusBadGateway)
			return
		}

		log.Printf("%s: brigade_id=%s reserved successfully", logTag, req.BrigadeID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ReserveResponse{OK: true})
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
