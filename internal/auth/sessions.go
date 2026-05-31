// Package auth handles participant sessions and the (future) authentication
// providers. In the MVP only guest identities exist; the Provider seam lets a
// JellyfinProvider drop in later without touching the rest of the app.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// ErrInvalidCookie is returned when a session cookie fails to verify.
var ErrInvalidCookie = errors.New("auth: invalid session cookie")

// Sessions signs and verifies the per-browser cookie that maps poll IDs to
// participant session tokens. A single cookie can carry membership in several
// polls at once (host of one, guest of another).
type Sessions struct {
	secret []byte
}

// NewSessions returns a Sessions signer using the given HMAC secret.
func NewSessions(secret []byte) *Sessions { return &Sessions{secret: secret} }

// Encode serializes the pollID->token map into a signed cookie value of the
// form "<base64url(json)>.<base64url(hmac)>".
func (s *Sessions) Encode(tokens map[string]string) (string, error) {
	payload, err := json.Marshal(tokens)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + s.sign(body), nil
}

// Decode verifies and parses a cookie value produced by Encode.
func (s *Sessions) Decode(cookie string) (map[string]string, error) {
	i := strings.LastIndexByte(cookie, '.')
	if i < 0 {
		return nil, ErrInvalidCookie
	}
	body, sig := cookie[:i], cookie[i+1:]
	if !hmac.Equal([]byte(sig), []byte(s.sign(body))) {
		return nil, ErrInvalidCookie
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, ErrInvalidCookie
	}
	tokens := map[string]string{}
	if err := json.Unmarshal(payload, &tokens); err != nil {
		return nil, ErrInvalidCookie
	}
	return tokens, nil
}

func (s *Sessions) sign(body string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
