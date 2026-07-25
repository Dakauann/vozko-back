package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/oauth"
	"vozko/infra/agent/mcp/vault"
	memrepo "vozko/infra/repositories/agent/mcp"
)

func newSigner() *oauth.Signer {
	return oauth.NewSigner(bytes.Repeat([]byte{9}, 32), 0)
}

type fixedResolver struct {
	cfg OAuth2Config
	err error
}

func (f fixedResolver) Resolve(context.Context, string, string, string) (OAuth2Config, error) {
	return f.cfg, f.err
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errors.New("rand") }

func TestStartOAuth2Validation(t *testing.T) {
	u := &StartOAuth2UseCase{Resolver: fixedResolver{cfg: OAuth2Config{}}, Signer: newSigner()}
	if _, err := u.Execute(context.Background(), StartOAuth2Input{}); err == nil {
		t.Fatal("empty ws")
	}
	if _, err := u.Execute(context.Background(), StartOAuth2Input{WorkspaceID: "ws"}); err == nil {
		t.Fatal("empty binding")
	}
	if _, err := u.Execute(context.Background(), StartOAuth2Input{WorkspaceID: "ws", BindingID: "b", Kind: "bad"}); err == nil {
		t.Fatal("bad kind")
	}
	u2 := &StartOAuth2UseCase{Resolver: fixedResolver{err: errors.New("bad")}, Signer: newSigner()}
	if _, err := u2.Execute(context.Background(), StartOAuth2Input{WorkspaceID: "ws", BindingID: "b", Kind: "builtin"}); err == nil {
		t.Fatal("resolver err")
	}
	u3 := &StartOAuth2UseCase{Resolver: fixedResolver{cfg: OAuth2Config{}}, Signer: newSigner()}
	if _, err := u3.Execute(context.Background(), StartOAuth2Input{WorkspaceID: "ws", BindingID: "b", Kind: "builtin"}); err == nil {
		t.Fatal("incomplete cfg")
	}
}

func TestStartOAuth2RandErr(t *testing.T) {
	u := &StartOAuth2UseCase{
		Resolver: fixedResolver{cfg: OAuth2Config{AuthzURL: "https://a", TokenURL: "https://t", ClientID: "c", RedirectURL: "https://r", UsePKCE: true}},
		Signer:   newSigner(),
		Rand:     badReader{},
	}
	if _, err := u.Execute(context.Background(), StartOAuth2Input{WorkspaceID: "ws", BindingID: "b", Kind: "builtin"}); err == nil {
		t.Fatal()
	}
}

func TestStartOAuth2AuthzURLWithQuery(t *testing.T) {
	u := &StartOAuth2UseCase{
		Resolver: fixedResolver{cfg: OAuth2Config{AuthzURL: "https://a?foo=1", TokenURL: "https://t", ClientID: "c", RedirectURL: "https://r", Scopes: []string{"s1"}}},
		Signer:   newSigner(),
	}
	out, err := u.Execute(context.Background(), StartOAuth2Input{WorkspaceID: "ws", BindingID: "b", Kind: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.AuthorizeURL, "&state=") {
		t.Fatalf("url=%s", out.AuthorizeURL)
	}
}

type doerFn func(*http.Request) (*http.Response, error)

func (d doerFn) Do(r *http.Request) (*http.Response, error) { return d(r) }

func mkResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

func TestCompleteOAuth2Validation(t *testing.T) {
	u := &CompleteOAuth2UseCase{Signer: newSigner()}
	if err := u.Execute(context.Background(), CompleteOAuth2Input{}); err == nil {
		t.Fatal("empty")
	}
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: "bad"}); err == nil {
		t.Fatal("bad state")
	}
}

func TestCompleteOAuth2ResolverErr(t *testing.T) {
	sig := newSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	u := &CompleteOAuth2UseCase{Signer: sig, Resolver: fixedResolver{err: errors.New("x")}}
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: st}); err == nil {
		t.Fatal()
	}
}

func TestCompleteOAuth2HTTPPaths(t *testing.T) {
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	bindings := memrepo.NewBuiltinBindingRepo()
	remotes := memrepo.NewRemoteServerRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "b", WorkspaceID: "ws", ServerKey: "b", Status: domainmcp.StatusPending})
	s, _ := domainmcp.NewRemoteMCPServer("r1", "ws", "r", "https://r.test", domainmcp.TransportStreamableHTTP)
	_ = remotes.Create(context.Background(), s)
	sig := newSigner()
	stBuiltin, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	stRemote, _ := sig.Sign(oauth.State{Kind: "remote", WorkspaceID: "ws", BindingID: "r1", Verifier: "v"})
	stBad, _ := sig.Sign(oauth.State{Kind: "weird", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	cfg := OAuth2Config{AuthzURL: "https://a", TokenURL: "https://t", ClientID: "c", ClientSecret: "cs", RedirectURL: "https://r", UsePKCE: true}

	u := &CompleteOAuth2UseCase{Signer: sig, Resolver: fixedResolver{cfg: cfg}, Bindings: bindings, Remotes: remotes, Vault: v,
		HTTP: doerFn(func(*http.Request) (*http.Response, error) { return nil, errors.New("neterr") })}
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stBuiltin}); err == nil {
		t.Fatal("transport err")
	}

	u.HTTP = doerFn(func(*http.Request) (*http.Response, error) { return mkResp(400, "bad"), nil })
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stBuiltin}); err == nil {
		t.Fatal("400")
	}

	u.HTTP = doerFn(func(*http.Request) (*http.Response, error) { return mkResp(200, "not-json"), nil })
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stBuiltin}); err == nil {
		t.Fatal("bad json")
	}

	u.HTTP = doerFn(func(*http.Request) (*http.Response, error) { return mkResp(200, `{"access_token":""}`), nil })
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stBuiltin}); err == nil {
		t.Fatal("empty token")
	}

	u.HTTP = doerFn(func(*http.Request) (*http.Response, error) {
		return mkResp(200, `{"access_token":"tok","expires_in":3600}`), nil
	})
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stBuiltin}); err != nil {
		t.Fatal(err)
	}

	u.HTTP = doerFn(func(*http.Request) (*http.Response, error) {
		return mkResp(200, `{"access_token":"tok"}`), nil
	})
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stRemote}); err != nil {
		t.Fatal(err)
	}

	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stBad}); err == nil {
		t.Fatal("kind")
	}

	stMissing, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "missing", Verifier: "v"})
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stMissing}); err == nil {
		t.Fatal("missing binding")
	}

	stMissing2, _ := sig.Sign(oauth.State{Kind: "remote", WorkspaceID: "ws", BindingID: "missingR", Verifier: "v"})
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: stMissing2}); err == nil {
		t.Fatal("missing remote")
	}
}

func TestCompleteOAuth2DefaultClient(t *testing.T) {
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "b", WorkspaceID: "ws", ServerKey: "b", Status: domainmcp.StatusPending})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "tok"})
	}))
	defer ts.Close()
	sig := newSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	u := &CompleteOAuth2UseCase{
		Signer: sig, Vault: v, Bindings: bindings, Remotes: memrepo.NewRemoteServerRepo(),
		Resolver: fixedResolver{cfg: OAuth2Config{AuthzURL: "x", TokenURL: ts.URL, ClientID: "c", RedirectURL: "x"}},
	}
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: st}); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteOAuth2NewRequestErr(t *testing.T) {
	sig := newSigner()
	st, _ := sig.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	u := &CompleteOAuth2UseCase{
		Signer: sig,
		Resolver: fixedResolver{cfg: OAuth2Config{
			AuthzURL: "x", TokenURL: "http://[::1]:bad", ClientID: "c", RedirectURL: "x",
		}},
	}
	if err := u.Execute(context.Background(), CompleteOAuth2Input{Code: "c", State: st}); err == nil {
		t.Fatal()
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abc", 10) != "abc" {
		t.Fatal()
	}
	if truncate("abcdef", 3) != "abc..." {
		t.Fatal()
	}
}

func TestEnvResolver(t *testing.T) {
	cat := NewStaticCatalog(
		domainmcp.BuiltinDescriptor{Key: "g", AuthSpec: domainmcp.BuiltinAuthSpec{
			Mode: domainmcp.AuthOAuth2, AuthzURL: "a", TokenURL: "t", ClientIDEnv: "CIDX", ClientSecretEnv: "CSECX", Scopes: []string{"s"}, UsePKCE: true,
		}},
		domainmcp.BuiltinDescriptor{Key: "x", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthAPIKey}},
	)
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "bg", WorkspaceID: "ws", ServerKey: "g"})
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "bx", WorkspaceID: "ws", ServerKey: "x"})
	e := &EnvResolver{Catalog: cat, Bindings: bindings, RedirectURL: "https://r"}
	t.Setenv("CIDX", "abc")
	t.Setenv("CSECX", "def")
	cfg, err := e.Resolve(context.Background(), "builtin", "ws", "bg")
	if err != nil || cfg.ClientID != "abc" {
		t.Fatalf("err=%v cfg=%+v", err, cfg)
	}
	if _, err := e.Resolve(context.Background(), "remote", "ws", "bg"); err == nil {
		t.Fatal("remote kind")
	}
	if _, err := e.Resolve(context.Background(), "builtin", "ws", "missing"); err == nil {
		t.Fatal("missing id")
	}
	if _, err := e.Resolve(context.Background(), "builtin", "ws", "bx"); err == nil {
		t.Fatal("non-oauth binding")
	}
}
