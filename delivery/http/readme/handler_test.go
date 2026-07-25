package readme

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/auth"
	userdomain "vozko/domain/user"
)

type stubUsers struct {
	user *userdomain.User
	err  error
}

func (stub *stubUsers) FindByEmail(string) (*userdomain.User, error) {
	return stub.user, stub.err
}

type stubTokens struct {
	pair *auth.TokenPair
	err  error
	user *userdomain.User
}

func (stub *stubTokens) Issue(user *userdomain.User) (*auth.TokenPair, error) {
	stub.user = user
	return stub.pair, stub.err
}

func signature(secret string, body webhookRequest, timestamp time.Time) string {
	canonical, _ := json.Marshal(body)
	milliseconds := timestamp.UnixMilli()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", milliseconds)
	_, _ = mac.Write(canonical)
	return fmt.Sprintf("t=%d,v0=%s", milliseconds, hex.EncodeToString(mac.Sum(nil)))
}

func TestPersonalizedDocsIssuesUserBearerToken(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "readme-secret"
	payload := webhookRequest{Email: "user@example.com"}
	user := &userdomain.User{ID: "user-1", Email: payload.Email}
	issuer := &stubTokens{pair: &auth.TokenPair{AccessToken: "access-token"}}
	handler := NewHandler(secret, &stubUsers{user: user}, issuer)
	handler.now = func() time.Time { return now }

	request := httptest.NewRequest(http.MethodPost, "/webhooks/readme", strings.NewReader(`{"email":"user@example.com"}`))
	request.Header.Set("ReadMe-Signature", signature(secret, payload, now))
	recorder := httptest.NewRecorder()
	handler.PersonalizedDocs(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if issuer.user != user {
		t.Fatal("token was not issued for the resolved user")
	}
	var response webhookResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Authorization != "Bearer access-token" {
		t.Fatalf("Authorization = %q, want %q", response.Authorization, "Bearer access-token")
	}
}

func TestPersonalizedDocsRejectsInvalidRequests(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "readme-secret"
	validPayload := webhookRequest{Email: "user@example.com"}
	validSignature := signature(secret, validPayload, now)
	tests := []struct {
		name      string
		body      string
		signature string
		secret    string
		users     UserFinder
		tokens    TokenIssuer
		want      int
	}{
		{name: "missing config", body: `{"email":"user@example.com"}`, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusServiceUnavailable},
		{name: "invalid json", body: `{`, secret: secret, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusBadRequest},
		{name: "oversized body", body: `{"email":"` + strings.Repeat("a", maxBodyBytes) + `"}`, secret: secret, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusBadRequest},
		{name: "missing signature", body: `{"email":"user@example.com"}`, secret: secret, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusUnauthorized},
		{name: "tampered body", body: `{"email":"other@example.com"}`, signature: validSignature, secret: secret, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusUnauthorized},
		{name: "expired signature", body: `{"email":"user@example.com"}`, signature: signature(secret, validPayload, now.Add(-31*time.Minute)), secret: secret, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusUnauthorized},
		{name: "future signature", body: `{"email":"user@example.com"}`, signature: signature(secret, validPayload, now.Add(6*time.Minute)), secret: secret, users: &stubUsers{}, tokens: &stubTokens{}, want: http.StatusUnauthorized},
		{name: "unknown user", body: `{"email":"user@example.com"}`, signature: validSignature, secret: secret, users: &stubUsers{err: userdomain.ErrNotFound}, tokens: &stubTokens{}, want: http.StatusNotFound},
		{name: "repository failure", body: `{"email":"user@example.com"}`, signature: validSignature, secret: secret, users: &stubUsers{err: errors.New("database unavailable")}, tokens: &stubTokens{}, want: http.StatusInternalServerError},
		{name: "token failure", body: `{"email":"user@example.com"}`, signature: validSignature, secret: secret, users: &stubUsers{user: &userdomain.User{ID: "user-1"}}, tokens: &stubTokens{err: errors.New("signing failed")}, want: http.StatusInternalServerError},
		{name: "empty token", body: `{"email":"user@example.com"}`, signature: validSignature, secret: secret, users: &stubUsers{user: &userdomain.User{ID: "user-1"}}, tokens: &stubTokens{pair: &auth.TokenPair{}}, want: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(test.secret, test.users, test.tokens)
			handler.now = func() time.Time { return now }
			request := httptest.NewRequest(http.MethodPost, "/webhooks/readme", strings.NewReader(test.body))
			request.Header.Set("ReadMe-Signature", test.signature)
			recorder := httptest.NewRecorder()

			handler.PersonalizedDocs(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}
