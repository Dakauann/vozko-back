package webchat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const identitySecretForTests = "site-secret"

func signJWT(t *testing.T, secret string, header, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	head := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(head))
	return head + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hs256() map[string]any { return map[string]any{"alg": "HS256", "typ": "JWT"} }

func TestVerifyIdentityTokenReadsTheSignedClaims(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	token := signJWT(t, identitySecretForTests, hs256(), map[string]any{
		"sub": "customer-42", "name": " Ana ", "email": "ana@example.com", "phone": "+55 11 99999-0000",
		"exp": now.Add(time.Hour).Unix(),
	})
	claims, err := VerifyIdentityToken(identitySecretForTests, token, now)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExternalID != "customer-42" || claims.Name != "Ana" || claims.Email != "ana@example.com" || claims.Phone != "+55 11 99999-0000" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestVerifyIdentityTokenRefusesEveryWeakToken(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	valid := map[string]any{"sub": "c1", "exp": now.Add(time.Hour).Unix()}
	with := func(k string, v any) map[string]any {
		out := map[string]any{"sub": "c1", "exp": now.Add(time.Hour).Unix()}
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
		return out
	}

	cases := map[string]struct {
		token string
		want  error
	}{
		"alg none":         {signJWT(t, identitySecretForTests, map[string]any{"alg": "none"}, valid), ErrIdentityTokenInvalid},
		"alg rs256":        {signJWT(t, identitySecretForTests, map[string]any{"alg": "RS256"}, valid), ErrIdentityTokenInvalid},
		"alg lowercase":    {signJWT(t, identitySecretForTests, map[string]any{"alg": "hs256"}, valid), ErrIdentityTokenInvalid},
		"wrong secret":     {signJWT(t, "other", hs256(), valid), ErrIdentityTokenInvalid},
		"missing sub":      {signJWT(t, identitySecretForTests, hs256(), with("sub", nil)), ErrIdentityTokenInvalid},
		"numeric sub":      {signJWT(t, identitySecretForTests, hs256(), with("sub", 42)), ErrIdentityTokenInvalid},
		"missing exp":      {signJWT(t, identitySecretForTests, hs256(), with("exp", nil)), ErrIdentityTokenInvalid},
		"expired":          {signJWT(t, identitySecretForTests, hs256(), with("exp", now.Add(-time.Hour).Unix())), ErrIdentityTokenExpired},
		"exp too far":      {signJWT(t, identitySecretForTests, hs256(), with("exp", now.Add(MaxIdentityTokenLifetime+time.Hour).Unix())), ErrIdentityTokenInvalid},
		"issued in future": {signJWT(t, identitySecretForTests, hs256(), with("iat", now.Add(time.Hour).Unix())), ErrIdentityTokenInvalid},
		"not before":       {signJWT(t, identitySecretForTests, hs256(), with("nbf", now.Add(time.Hour).Unix())), ErrIdentityTokenInvalid},
		"two parts":        {"abc.def", ErrIdentityTokenInvalid},
		"oversized":        {string(make([]byte, MaxIdentityTokenBytes+1)), ErrIdentityTokenInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyIdentityToken(identitySecretForTests, tc.token, now); !errors.Is(err, tc.want) {
				t.Fatalf("VerifyIdentityToken = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifyIdentityTokenNeedsASecret(t *testing.T) {
	if _, err := VerifyIdentityToken("", "a.b.c", time.Now()); !errors.Is(err, ErrIdentitySecretMissing) {
		t.Fatalf("empty secret = %v", err)
	}
}

func TestGeneratedIdentitySecretsAreLongAndDistinct(t *testing.T) {
	a, err := GenerateIdentitySecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateIdentitySecret()
	if len(a) < 40 || a == b {
		t.Fatalf("secrets %q %q", a, b)
	}
}
