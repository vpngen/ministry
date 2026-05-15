package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	jwtsvc "github.com/vpngen/keydesk/pkg/jwt"
)

// BearerAuthTransport injects a signed JWT into every outgoing HTTP request.
// The token is refreshed automatically before expiry.
type BearerAuthTransport struct {
	token     string
	expiredAt time.Time

	issuer    *jwtsvc.KeydeskTokenIssuer
	Transport http.RoundTripper
}

func NewBearerAuthTransport(issuer *jwtsvc.KeydeskTokenIssuer, transport http.RoundTripper) *BearerAuthTransport {
	return &BearerAuthTransport{
		issuer:    issuer,
		Transport: transport,
	}
}

func (t *BearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := cloneRequest(req)

	if t.token == "" || time.Now().After(t.expiredAt) {
		token, err := t.issuer.Sign(t.issuer.CreateToken(10*time.Minute, false))
		if err != nil {
			return nil, fmt.Errorf("sign token: %w", err)
		}

		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid token")
		}

		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("decode token payload: %w", err)
		}

		var data jwt.MapClaims
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, fmt.Errorf("unmarshal token payload: %w", err)
		}

		exp, err := data.GetExpirationTime()
		if err != nil {
			return nil, fmt.Errorf("get token expiration time: %w", err)
		}

		if exp == nil {
			return nil, fmt.Errorf("token has no expiration time")
		}

		t.token = token
		t.expiredAt = exp.Add(-1 * time.Minute)
	}

	req2.Header.Set("Authorization", "Bearer "+t.token)

	return t.underlying().RoundTrip(req2)
}

func (t *BearerAuthTransport) Token() string {
	return t.token
}

func (t *BearerAuthTransport) underlying() http.RoundTripper {
	if t.Transport != nil {
		return t.Transport
	}

	return http.DefaultTransport
}

func cloneRequest(r *http.Request) *http.Request {
	r2 := new(http.Request)
	*r2 = *r
	r2.Header = make(http.Header, len(r.Header))

	for k, s := range r.Header {
		r2.Header[k] = append([]string(nil), s...)
	}

	return r2
}
