package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/oauth"
	"vozko/infra/agent/mcp/vault"
	memrepo "vozko/infra/repositories/agent/mcp"
)

func auditSigner() *oauth.Signer { return oauth.NewSigner(bytes.Repeat([]byte{7}, 32), 0) }

func TestAuditOAuth_Authorize_RequiredParams(t *testing.T) {
	sig := auditSigner()
	uc := &StartOAuth2UseCase{
		Signer: sig,
		Resolver: fixedResolver{cfg: OAuth2Config{
			AuthzURL:    "https://auth.example/authorize",
			TokenURL:    "https://auth.example/token",
			ClientID:    "cid-abc",
			RedirectURL: "https://app.vozko.test/callback",
			Scopes:      []string{"mcp.read", "mcp.write"},
			UsePKCE:     true,
			Resource:    "https://mcp.vozko.test",
		}},
	}
	out, err := uc.Execute(context.Background(), StartOAuth2Input{
		Kind: "builtin", WorkspaceID: "ws", BindingID: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(out.AuthorizeURL)
	if err != nil {
		t.Fatalf("URL parse: %v", err)
	}
	q := u.Query()
	mustEq := map[string]string{
		"response_type":         "code",
		"client_id":             "cid-abc",
		"redirect_uri":          "https://app.vozko.test/callback",
		"scope":                 "mcp.read mcp.write",
		"code_challenge_method": "S256",
		"resource":              "https://mcp.vozko.test",
	}
	for k, want := range mustEq {
		if got := q.Get(k); got != want {
			t.Fatalf("authorize %q = %q want %q", k, got, want)
		}
	}
	for _, k := range []string{"state", "code_challenge"} {
		if q.Get(k) == "" {
			t.Fatalf("authorize missing %q: %s", k, out.AuthorizeURL)
		}
	}
}

func TestAuditOAuth_Authorize_NoPKCE_Omits(t *testing.T) {
	uc := &StartOAuth2UseCase{
		Signer: auditSigner(),
		Resolver: fixedResolver{cfg: OAuth2Config{
			AuthzURL:    "https://auth.example/authorize",
			TokenURL:    "https://auth.example/token",
			ClientID:    "cid",
			RedirectURL: "https://app/cb",
		}},
	}
	out, err := uc.Execute(context.Background(), StartOAuth2Input{
		Kind: "remote", WorkspaceID: "ws", BindingID: "r",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(out.AuthorizeURL)
	if u.Query().Get("code_challenge") != "" {
		t.Fatal("code_challenge must be absent when PKCE disabled")
	}
}

func TestAuditOAuth_Token_Exchange_PostsSpecCompliantForm(t *testing.T) {
	var gotCT string
	var gotForm url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "tok", ExpiresIn: 3600})
	}))
	defer ts.Close()

	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{
		ID: "b", WorkspaceID: "ws", ServerKey: "b", Status: domainmcp.StatusPending,
	})
	sig := auditSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "verifier-abc"})
	uc := &CompleteOAuth2UseCase{
		Signer:   sig,
		Bindings: bindings,
		Remotes:  memrepo.NewRemoteServerRepo(),
		Vault:    v,
		Resolver: fixedResolver{cfg: OAuth2Config{
			AuthzURL:     "https://auth.example/authorize",
			TokenURL:     ts.URL,
			ClientID:     "cid",
			ClientSecret: "cs",
			RedirectURL:  "https://app/cb",
			UsePKCE:      true,
			Resource:     "https://mcp.vozko.test",
		}},
	}
	if err := uc.Execute(context.Background(), CompleteOAuth2Input{Code: "xyz", State: st}); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(gotCT, "application/x-www-form-urlencoded") {
		t.Fatalf("token content-type = %q, want application/x-www-form-urlencoded", gotCT)
	}
	mustEq := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "xyz",
		"redirect_uri":  "https://app/cb",
		"client_id":     "cid",
		"client_secret": "cs",
		"code_verifier": "verifier-abc",
		"resource":      "https://mcp.vozko.test",
	}
	for k, want := range mustEq {
		if got := gotForm.Get(k); got != want {
			t.Fatalf("token form %q = %q want %q", k, got, want)
		}
	}
}

func TestAuditOAuth_Token_StoresSealedCredential(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "tok-plain", ExpiresIn: 7200})
	}))
	defer ts.Close()

	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{
		ID: "b", WorkspaceID: "ws", ServerKey: "b", Status: domainmcp.StatusPending,
	})
	sig := auditSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	uc := &CompleteOAuth2UseCase{
		Signer: sig, Bindings: bindings, Remotes: memrepo.NewRemoteServerRepo(), Vault: v,
		Resolver: fixedResolver{cfg: OAuth2Config{AuthzURL: "x", TokenURL: ts.URL, ClientID: "c", RedirectURL: "r"}},
	}
	if err := uc.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: st}); err != nil {
		t.Fatal(err)
	}
	b, err := bindings.GetByID(context.Background(), "ws", "b")
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != domainmcp.StatusConnected {
		t.Fatalf("status=%v", b.Status)
	}
	if b.Credential == nil || b.Credential.Mode != domainmcp.AuthOAuth2 {
		t.Fatalf("credential not stored: %+v", b.Credential)
	}

	if bytes.Equal(b.Credential.Cipher, []byte("tok-plain")) {
		t.Fatal("credential must be sealed, not plaintext")
	}
	if b.Credential.ExpiresAt == nil {
		t.Fatal("ExpiresAt must be set when expires_in is provided")
	}
	if b.Credential.RefreshHint == nil || !b.Credential.RefreshHint.Before(*b.Credential.ExpiresAt) {
		t.Fatalf("RefreshHint must precede ExpiresAt: %+v / %+v", b.Credential.RefreshHint, b.Credential.ExpiresAt)
	}
}

func TestAuditOAuth_State_Signed_TamperRejected(t *testing.T) {
	sig := auditSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})

	bad := []byte(st)
	bad[len(bad)-1] ^= 0x01
	uc := &CompleteOAuth2UseCase{Signer: sig}
	if err := uc.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: string(bad)}); err == nil {
		t.Fatal("tampered state must be rejected")
	}
}

func TestAuditOAuth_Non2xx_PropagatesError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer ts.Close()

	sig := auditSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	uc := &CompleteOAuth2UseCase{
		Signer:   sig,
		Resolver: fixedResolver{cfg: OAuth2Config{AuthzURL: "x", TokenURL: ts.URL, ClientID: "c", RedirectURL: "r"}},
	}
	err := uc.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: st})
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("want error surfacing invalid_grant, got %v", err)
	}
}
