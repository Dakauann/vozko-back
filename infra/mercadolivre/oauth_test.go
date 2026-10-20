package mercadolivre

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

const tokenResponseDocExample = `{
    "access_token": "APP_USR-123456-090515-8cc4448aac10d5105474e1351-1234567",
    "token_type": "bearer",
    "expires_in": 21600,
    "scope": "offline_access read write",
    "user_id": 1234567,
    "refresh_token": "TG-5b9032b4e23464aed1f959f-1234567"
}`

const invalidGrantDocExample = `{
    "error_description": "Error validating grant. Your authorization code or refresh token may be expired or it was already used",
    "error": "invalid_grant",
    "status": 400,
    "cause": []
}`

func newOAuth(t *testing.T, fs *fakeServer, limiters *fakeLimiters) *OAuthService {
	t.Helper()
	s, err := NewOAuthService(newTestClient(t, fs, limiters), OAuthConfig{
		AppID:        "1620218256833906",
		ClientSecret: "client-secret-value",
		RedirectURI:  "https://api.example.com/oauth/mercadolivre/callback",
	})
	if err != nil {
		t.Fatalf("NewOAuthService: %v", err)
	}
	return s
}

func TestNewOAuthServiceRefusesIncompleteConfig(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	c := newTestClient(t, fs, newFakeLimiters())
	cases := []OAuthConfig{
		{ClientSecret: "s", RedirectURI: "https://a.example/cb"},
		{AppID: "1", RedirectURI: "https://a.example/cb"},
		{AppID: "1", ClientSecret: "s"},
		{AppID: "1", ClientSecret: "s", RedirectURI: "http://a.example/cb"},
		{AppID: "1", ClientSecret: "s", RedirectURI: "https://a.example/cb?x=1"},
		{AppID: "abc", ClientSecret: "s", RedirectURI: "https://a.example/cb"},
	}
	for i, cfg := range cases {
		if _, err := NewOAuthService(c, cfg); err == nil {
			t.Errorf("config %d accepted", i)
		}
	}
	if _, err := NewOAuthService(nil, OAuthConfig{AppID: "1", ClientSecret: "s", RedirectURI: "https://a.example/cb"}); err == nil {
		t.Error("nil client accepted")
	}
}

func TestTheAuthorizeURLCanPointAtAnotherHostForEndToEndRuns(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	c := newTestClient(t, fs, newFakeLimiters())
	base := OAuthConfig{AppID: "1", ClientSecret: "s", RedirectURI: "https://a.example/cb"}
	for _, bad := range []string{"auth.example/authorization", "ftp://auth.example/authorization", "https://", "https://auth.example/authorization?x=1", "https://auth.example/a#b"} {
		cfg := base
		cfg.AuthorizeURL = bad
		if _, err := NewOAuthService(c, cfg); err == nil {
			t.Errorf("authorize url %q accepted", bad)
		}
	}
	challenge, _ := PKCEChallenge("dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8")
	cases := map[string]string{
		"":                                     DefaultAuthorizeURL,
		"http://127.0.0.1:8089/authorization/": "http://127.0.0.1:8089/authorization",
	}
	for raw, want := range cases {
		cfg := base
		cfg.AuthorizeURL = raw
		s, err := NewOAuthService(c, cfg)
		if err != nil {
			t.Fatalf("authorize url %q: %v", raw, err)
		}
		got, err := s.AuthorizeURL("state", challenge)
		if err != nil || !strings.HasPrefix(got, want+"?") {
			t.Errorf("authorize url %q gave %q, %v", raw, got, err)
		}
	}
}

func TestPKCE(t *testing.T) {
	verifier, err := NewPKCEVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`).MatchString(verifier) {
		t.Fatalf("verifier %q is not RFC 7636 shaped", verifier)
	}
	other, _ := NewPKCEVerifier()
	if other == verifier {
		t.Fatal("verifiers must be random")
	}
	got, err := PKCEChallenge("dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8")
	if err != nil || got != "7yZLCBa4I_ewp7VK907jDq6mNAj85KuBpekfLaxcHgY" {
		t.Fatalf("PKCEChallenge = %q, %v", got, err)
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 129), strings.Repeat("a", 42) + "!"} {
		if _, err := PKCEChallenge(bad); err == nil {
			t.Errorf("PKCEChallenge(%q) accepted", bad)
		}
	}
}

func TestAuthorizeURL(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newOAuth(t, fs, newFakeLimiters())
	challenge, _ := PKCEChallenge("dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8")
	raw, err := s.AuthorizeURL("signed-state", challenge)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "auth.mercadolivre.com.br" || u.Path != "/authorization" {
		t.Fatalf("authorize url = %s", raw)
	}
	q := u.Query()
	want := map[string]string{
		"response_type":         "code",
		"client_id":             "1620218256833906",
		"redirect_uri":          "https://api.example.com/oauth/mercadolivre/callback",
		"state":                 "signed-state",
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if len(q) != len(want) {
		t.Fatalf("unexpected parameters: %v", q)
	}
	if _, err := s.AuthorizeURL("", challenge); err == nil {
		t.Fatal("an empty state must be refused")
	}
	if _, err := s.AuthorizeURL("state", "not a challenge"); err == nil {
		t.Fatal("a malformed challenge must be refused")
	}
}

func TestExchangeCode(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, tokenResponseDocExample))
	limiters := newFakeLimiters()
	s := newOAuth(t, fs, limiters)
	grant, err := s.ExchangeCode(context.Background(), "TG-61828b7fffcc9a001b4bc890-314029626", "dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if grant.AccessToken != "APP_USR-123456-090515-8cc4448aac10d5105474e1351-1234567" ||
		grant.RefreshToken != "TG-5b9032b4e23464aed1f959f-1234567" ||
		grant.UserID != "1234567" || grant.ExpiresIn != 6*time.Hour ||
		strings.Join(grant.Scopes, " ") != "offline_access read write" {
		t.Fatalf("grant = %+v", grant)
	}
	r := fs.last()
	if r.Method != http.MethodPost || r.Path != "/oauth/token" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
	if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("Authorization") != "" {
		t.Fatalf("headers = %v", r.Header)
	}
	form, _ := url.ParseQuery(r.Body)
	wantForm := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     "1620218256833906",
		"client_secret": "client-secret-value",
		"code":          "TG-61828b7fffcc9a001b4bc890-314029626",
		"redirect_uri":  "https://api.example.com/oauth/mercadolivre/callback",
		"code_verifier": "dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8",
	}
	for k, v := range wantForm {
		if form.Get(k) != v {
			t.Errorf("form %s = %q, want %q", k, form.Get(k), v)
		}
	}
	if len(r.Query) != 0 {
		t.Fatalf("secrets must travel in the body, query = %v", r.Query)
	}
	keys := limiters.keys()
	if len(keys) != 2 || keys[0] != "ml:exchange" || keys[1] != "ml:app:oauth" {
		t.Fatalf("limiter keys = %v", keys)
	}
}

func TestExchangeCodeRefusals(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, tokenResponseDocExample))
	s := newOAuth(t, fs, newFakeLimiters())
	if _, err := s.ExchangeCode(context.Background(), "", "dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8"); err == nil {
		t.Fatal("empty code accepted")
	}
	if _, err := s.ExchangeCode(context.Background(), "TG-1", "short"); err == nil {
		t.Fatal("malformed verifier accepted")
	}
	if fs.count() != 0 {
		t.Fatal("refusals must not reach Mercado Livre")
	}
}

func TestExchangeCodeMapsTheOperatorError(t *testing.T) {
	fs := newFakeServer(t, jsonReply(400, `{"error":"invalid_operator_user_id","message":"operator","status":400}`))
	s := newOAuth(t, fs, newFakeLimiters())
	_, err := s.ExchangeCode(context.Background(), "TG-1", "dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8")
	if !IsOperatorUser(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestExchangeCodeRefusesAnIncompleteTokenAnswer(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"","refresh_token":"TG","user_id":1,"expires_in":21600}`,
		`{"access_token":"A","refresh_token":"","user_id":1,"expires_in":21600}`,
		`{"access_token":"A","refresh_token":"TG","user_id":"me","expires_in":21600}`,
		`{"access_token":"A","refresh_token":"TG","user_id":1,"expires_in":0}`,
		`not json`,
	} {
		fs := newFakeServer(t, jsonReply(200, body))
		s := newOAuth(t, fs, newFakeLimiters())
		if _, err := s.ExchangeCode(context.Background(), "TG-1", "dBjftJeZ4CVP-mJ92K9xqv0d9w8i8f6iSJ2pTJgYbT8"); err == nil {
			t.Errorf("token answer %s accepted", body)
		}
	}
}

func TestRefresh(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, tokenResponseDocExample))
	limiters := newFakeLimiters()
	s := newOAuth(t, fs, limiters)
	grant, err := s.Refresh(context.Background(), mldomain.RefreshCredentials{UserID: "1234567", RefreshToken: "TG-old"})
	if err != nil || grant.RefreshToken != "TG-5b9032b4e23464aed1f959f-1234567" {
		t.Fatalf("Refresh = %+v, %v", grant, err)
	}
	form, _ := url.ParseQuery(fs.last().Body)
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "TG-old" ||
		form.Get("client_id") != "1620218256833906" || form.Get("client_secret") != "client-secret-value" {
		t.Fatalf("form = %v", form)
	}
	if keys := limiters.keys(); keys[0] != "ml:1234567" {
		t.Fatalf("refresh must count against the account: %v", keys)
	}
	if _, err := s.Refresh(context.Background(), mldomain.RefreshCredentials{UserID: "x", RefreshToken: "TG"}); err == nil {
		t.Fatal("bad user accepted")
	}
	if _, err := s.Refresh(context.Background(), mldomain.RefreshCredentials{UserID: "1", RefreshToken: ""}); err == nil {
		t.Fatal("empty refresh token accepted")
	}
}

func TestRefreshInvalidGrantIsAuth(t *testing.T) {
	fs := newFakeServer(t, jsonReply(400, invalidGrantDocExample))
	s := newOAuth(t, fs, newFakeLimiters())
	_, err := s.Refresh(context.Background(), mldomain.RefreshCredentials{UserID: "1234567", RefreshToken: "TG-old"})
	if !IsAuth(err) || IsRetryable(err) {
		t.Fatalf("err = %v", err)
	}
	if fs.count() != 1 {
		t.Fatalf("a dead refresh token must not be retried, attempts = %d", fs.count())
	}
}

func TestRefreshIsNotRetriedOnAServerError(t *testing.T) {
	fs := newFakeServer(t, jsonReply(500, `{}`))
	s := newOAuth(t, fs, newFakeLimiters())
	_, err := s.Refresh(context.Background(), mldomain.RefreshCredentials{UserID: "1234567", RefreshToken: "TG-old"})
	if err == nil || fs.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
}

func TestRevoke(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newOAuth(t, fs, newFakeLimiters())
	if err := s.Revoke(context.Background(), sellerCreds); err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodDelete || r.Path != "/users/415458330/applications/1620218256833906" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
	assertBearer(t, r)
	if err := s.Revoke(context.Background(), mldomain.Credentials{UserID: "415458330"}); !errors.Is(err, mldomain.ErrCredentialsInvalid) {
		t.Fatalf("err = %v", err)
	}
}
