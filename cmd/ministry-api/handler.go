package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReserveRequest is the body sent by tgbot.
// brigade_id is the obfuscated UUID returned by reqvipid.
// user_identity is the Telegram ChatID.
type ReserveRequest struct {
	BrigadeID    uuid.UUID `json:"brigade_id"`
	UserIdentity string    `json:"user_identity"`
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
// Matches the PaidUser shape the VIP server uses in fetchPaidUsers responses.
// Confirm exact field names with Oleg if needed.
type vipReservePayload struct {
	UserID       uuid.UUID `json:"user_id"`
	UserIdentity string    `json:"user_idenity"` // note: typo is intentional, matches Oleg's API
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

		// --- auth ---
		authHeader := r.Header.Get("Authorization")
		parts := strings.SplitN(authHeader, " ", 2)

		if len(parts) != 2 || parts[0] != "Bearer" {
			writeError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		tokenBytes, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(parts[1])
		if err != nil {
			writeError(w, "invalid token encoding", http.StatusUnauthorized)
			return
		}

		_, ok, err := checkToken(ctx, db, defaultBrigadesSchema, tokenBytes)
		if err != nil || !ok {
			writeError(w, "access denied", http.StatusUnauthorized)
			return
		}

		// --- parse body ---
		var req ReserveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.BrigadeID == uuid.Nil {
			writeError(w, "brigade_id required", http.StatusBadRequest)
			return
		}

		if req.UserIdentity == "" {
			writeError(w, "user_identity required", http.StatusBadRequest)
			return
		}

		// --- call VIP server ---
		// brigade_id from tgbot is already obfuscated — that's the user_id the VIP server expects.
		payload, err := json.Marshal(vipReservePayload{
			UserID:       req.BrigadeID,
			UserIdentity: req.UserIdentity,
		})
		if err != nil {
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		vipReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			fmt.Sprintf("https://%s/partner_api/reserve", cfg.vipEndpoint),
			bytes.NewReader(payload))
		if err != nil {
			writeError(w, "internal error", http.StatusInternalServerError)
			return
		}

		vipReq.Header.Set("Content-Type", "application/json")

		resp, err := c.Do(vipReq)
		if err != nil {
			writeError(w, fmt.Sprintf("vip server unreachable: %s", err), http.StatusBadGateway)
			return
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			writeError(w, "failed to read vip server response", http.StatusBadGateway)
			return
		}

		if resp.StatusCode != http.StatusOK {
			writeError(w, fmt.Sprintf("vip server: %s", body), http.StatusBadGateway)
			return
		}

		var vipResp vipReserveResponse
		if err := json.Unmarshal(body, &vipResp); err != nil {
			writeError(w, "invalid response from vip server", http.StatusBadGateway)
			return
		}

		if vipResp.Result != "success" {
			writeError(w, fmt.Sprintf("vip server: %s", vipResp.Result), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ReserveResponse{OK: true})
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
