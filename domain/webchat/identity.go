package webchat

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const (
	MaxIdentityTokenBytes    = 4096
	MaxIdentityTokenLifetime = 24 * time.Hour
	MaxExternalIDBytes       = 128
	identitySecretBytes      = 32
)

type IdentityClaims struct {
	ExternalID string
	Name       string
	Email      string
	Phone      string
	ExpiresAt  time.Time
}

type identityHeader struct {
	Alg string `json:"alg"`
}

type identityPayload struct {
	Sub   json.RawMessage `json:"sub"`
	Name  string          `json:"name"`
	Email string          `json:"email"`
	Phone string          `json:"phone"`
	Exp   *float64        `json:"exp"`
	Iat   *float64        `json:"iat"`
	Nbf   *float64        `json:"nbf"`
}

func VerifyIdentityToken(secret, token string, now time.Time) (IdentityClaims, error) {
	if secret == "" {
		return IdentityClaims{}, ErrIdentitySecretMissing
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > MaxIdentityTokenBytes {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}

	var header identityHeader
	if !decodeSegment(parts[0], &header) || header.Alg != "HS256" {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}

	var payload identityPayload
	if !decodeSegment(parts[1], &payload) {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}
	var sub string
	if err := json.Unmarshal(payload.Sub, &sub); err != nil {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}
	sub = strings.TrimSpace(sub)
	if sub == "" || len(sub) > MaxExternalIDBytes || payload.Exp == nil {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}

	expires := unixSeconds(*payload.Exp)
	if now.After(expires) {
		return IdentityClaims{}, ErrIdentityTokenExpired
	}
	if expires.After(now.Add(MaxIdentityTokenLifetime + tokenClockSkew)) {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}
	if payload.Iat != nil && unixSeconds(*payload.Iat).After(now.Add(tokenClockSkew)) {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}
	if payload.Nbf != nil && unixSeconds(*payload.Nbf).After(now.Add(tokenClockSkew)) {
		return IdentityClaims{}, ErrIdentityTokenInvalid
	}

	return IdentityClaims{
		ExternalID: sub,
		Name:       strings.TrimSpace(payload.Name),
		Email:      strings.TrimSpace(payload.Email),
		Phone:      strings.TrimSpace(payload.Phone),
		ExpiresAt:  expires,
	}, nil
}

func decodeSegment(segment string, into any) bool {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, into) == nil
}

func unixSeconds(v float64) time.Time {
	return time.Unix(int64(v), 0).UTC()
}

func GenerateIdentitySecret() (string, error) {
	return randomToken(identitySecretBytes)
}

func GeneratePublicKey() (string, error) {
	return randomToken(18)
}

func randomToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
