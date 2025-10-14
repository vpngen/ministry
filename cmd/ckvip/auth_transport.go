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

const DefaultRequestDelay = 100 * time.Millisecond

func NewBearerAuthTransport(issuer *jwtsvc.KeydeskTokenIssuer, transport http.RoundTripper) *BearerAuthTransport {
	if issuer == nil {
		return nil
	}

	return &BearerAuthTransport{
		issuer:    issuer,
		Transport: transport,
	}
}

// BearerAuthTransport is an http.RoundTripper that authenticates all requests
// using HTTP Bearer Authentication with the provided Token.
type BearerAuthTransport struct {
	token      string
	expired_at time.Time

	issuer *jwtsvc.KeydeskTokenIssuer

	// Transport is the underlying HTTP transport to use when making requests.
	// It will default to http.DefaultTransport if nil.
	Transport http.RoundTripper
}

// RoundTrip implements the RoundTripper interface.  We just add the
// basic auth information and return the RoundTripper for this transport type.
func (t *BearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := cloneRequest(req) // per RoundTripper contract

	if t.token == "" || time.Now().After(t.expired_at) {
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
		t.expired_at = exp.Add(-1 * time.Minute)
	}

	req2.Header.Set("Authorization", "Bearer "+t.token)

	return t.transport().RoundTrip(req2)
}

func (t *BearerAuthTransport) transport() http.RoundTripper {
	if t.Transport != nil {
		return t.Transport
	}

	return http.DefaultTransport
}

// cloneRequest returns a clone of the provided *http.Request.
// The clone is a shallow copy of the struct and its Header map.
func cloneRequest(r *http.Request) *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *r

	// deep copy of the Header
	r2.Header = make(http.Header, len(r.Header))
	for k, s := range r.Header {
		r2.Header[k] = append([]string(nil), s...)
	}

	return r2
}
