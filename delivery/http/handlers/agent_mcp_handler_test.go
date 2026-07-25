package handlers

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

	"github.com/gorilla/mux"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/oauth"
	"vozko/infra/agent/mcp/vault"
	"vozko/infra/http/middleware"
	memrepo "vozko/infra/repositories/agent/mcp"
	ucmcp "vozko/usecases/agent/mcp"
)

func wsCtx(r *http.Request, ws string) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, ws)
	return r.WithContext(ctx)
}

type env struct {
	h        *AgentMCPHandler
	remoteH  *AgentMCPRemoteHandler
	catalog  *ucmcp.StaticCatalog
	bindings *memrepo.BuiltinBindingRepo
	remotes  *memrepo.RemoteServerRepo
	cache    *memrepo.ToolCacheRepo
	vault    *vault.Vault
}

type fakeProber struct {
	tools []domainmcp.Tool
	err   error
}

func (f fakeProber) Probe(context.Context, string, ucmcp.ProbeAuth) ([]domainmcp.Tool, error) {
	return f.tools, f.err
}

func newEnv(t *testing.T) *env {
	t.Helper()
	v, err := vault.New(bytes.Repeat([]byte{3}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	desc := domainmcp.BuiltinDescriptor{
		Key:         "notion",
		DisplayName: "Notion",
		AuthSpec:    domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthAPIKey},
	}
	descOAuth := domainmcp.BuiltinDescriptor{
		Key:         "gcal",
		DisplayName: "Google Calendar",
		AuthSpec: domainmcp.BuiltinAuthSpec{
			Mode:            domainmcp.AuthOAuth2,
			AuthzURL:        "https://example.com/authorize",
			TokenURL:        "https://example.com/token",
			Scopes:          []string{"calendar.read"},
			UsePKCE:         true,
			ClientIDEnv:     "TEST_CID",
			ClientSecretEnv: "TEST_CSEC",
		},
	}
	cat := ucmcp.NewStaticCatalog(desc, descOAuth)
	bindings := memrepo.NewBuiltinBindingRepo()
	remotes := memrepo.NewRemoteServerRepo()
	cache := memrepo.NewToolCacheRepo()
	enable := &ucmcp.EnableBuiltinUseCase{Catalog: cat, Bindings: bindings, Vault: v}
	configure := &ucmcp.ConfigureBuiltinAuthUseCase{Catalog: cat, Bindings: bindings, Vault: v}
	register := &ucmcp.RegisterRemoteUseCase{Remotes: remotes, Cache: cache, Vault: v, Prober: fakeProber{tools: []domainmcp.Tool{{Name: "t"}}}}
	signer := oauth.NewSigner(bytes.Repeat([]byte{7}, 32), 0)
	start := &ucmcp.StartOAuth2UseCase{
		Resolver: &ucmcp.EnvResolver{Catalog: cat, Bindings: bindings, RedirectURL: "https://vozko.test/callback"},
		Signer:   signer,
	}
	t.Setenv("TEST_CID", "cid")
	t.Setenv("TEST_CSEC", "csec")
	return &env{
		h:        NewAgentMCPHandler(cat, enable, configure, bindings, start),
		remoteH:  NewAgentMCPRemoteHandler(register, start, remotes, cache),
		catalog:  cat,
		bindings: bindings,
		remotes:  remotes,
		cache:    cache,
		vault:    v,
	}
}

func doJSON(t *testing.T, handler http.HandlerFunc, method, path, body, ws string, vars map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if ws != "" {
		r = wsCtx(r, ws)
	}
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func TestListCatalog(t *testing.T) {
	e := newEnv(t)
	w := doJSON(t, e.h.ListCatalog, "GET", "/", "", "ws", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if !strings.Contains(w.Body.String(), "notion") {
		t.Fatal(w.Body.String())
	}
}

func TestListBindingsFlow(t *testing.T) {
	e := newEnv(t)

	w := doJSON(t, e.h.ListBindings, "GET", "/", "", "", nil)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.ListBindings, "GET", "/", "", "ws", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}

	_ = e.bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "b1", WorkspaceID: "ws", ServerKey: "notion", Status: domainmcp.StatusConnected})
	w = doJSON(t, e.h.ListBindings, "GET", "/", "", "ws", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestEnableBuiltin(t *testing.T) {
	e := newEnv(t)

	w := doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"notion"}`, "", nil)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.Enable, "POST", "/", `not-json`, "ws", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.Enable, "POST", "/", `{}`, "ws", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"nope"}`, "ws", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"notion"}`, "ws", nil)
	if w.Code != 201 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConfigureAPIKey(t *testing.T) {
	e := newEnv(t)

	w0 := doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"notion"}`, "ws", nil)
	var created map[string]any
	_ = json.Unmarshal(w0.Body.Bytes(), &created)
	bid, _ := created["id"].(string)

	w := doJSON(t, e.h.ConfigureAPIKey, "PUT", "/", `{"apiKey":"k"}`, "", map[string]string{"id": bid})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.ConfigureAPIKey, "PUT", "/", `not-json`, "ws", map[string]string{"id": bid})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.ConfigureAPIKey, "PUT", "/", `{"apiKey":"k"}`, "ws", map[string]string{"id": "missing"})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.ConfigureAPIKey, "PUT", "/", `{"apiKey":"secret"}`, "ws", map[string]string{"id": bid})
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteBinding(t *testing.T) {
	e := newEnv(t)

	w := doJSON(t, e.h.Delete, "DELETE", "/", "", "", map[string]string{"id": "any"})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.Delete, "DELETE", "/", "", "ws", map[string]string{"id": "missing"})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}

	w0 := doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"notion"}`, "ws", nil)
	var created map[string]any
	_ = json.Unmarshal(w0.Body.Bytes(), &created)
	bid, _ := created["id"].(string)
	w = doJSON(t, e.h.Delete, "DELETE", "/", "", "ws", map[string]string{"id": bid})
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

func TestStartOAuth2(t *testing.T) {
	e := newEnv(t)

	w0 := doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"gcal"}`, "ws", nil)
	var created map[string]any
	_ = json.Unmarshal(w0.Body.Bytes(), &created)
	gcalID, _ := created["id"].(string)

	w1 := doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"notion"}`, "ws", nil)
	var created2 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &created2)
	notionID, _ := created2["id"].(string)

	w := doJSON(t, e.h.StartOAuth2, "POST", "/", "", "", map[string]string{"id": gcalID})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.StartOAuth2, "POST", "/", "", "ws", map[string]string{"id": "missing"})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.StartOAuth2, "POST", "/", "", "ws", map[string]string{"id": notionID})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.h.StartOAuth2, "POST", "/", "", "ws", map[string]string{"id": gcalID})
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if !strings.HasPrefix(out["authorizeUrl"], "https://example.com/authorize?") {
		t.Fatalf("url=%s", out["authorizeUrl"])
	}
}

type failingBindings struct{}

func (failingBindings) Upsert(context.Context, *domainmcp.BuiltinBinding) error { return nil }
func (failingBindings) GetByID(context.Context, string, string) (*domainmcp.BuiltinBinding, error) {
	return nil, nil
}
func (failingBindings) ListByWorkspace(context.Context, string) ([]*domainmcp.BuiltinBinding, error) {
	return nil, errors.New("boom")
}
func (failingBindings) Delete(context.Context, string, string) error { return nil }

func TestListBindingsRepoError(t *testing.T) {
	h := NewAgentMCPHandler(ucmcp.NewStaticCatalog(), nil, nil, failingBindings{}, nil)
	w := doJSON(t, h.ListBindings, "GET", "/", "", "ws", nil)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}

func TestWriteDomainErrorExhaustive(t *testing.T) {
	h := &AgentMCPHandler{}
	cases := []struct {
		err  error
		code int
	}{
		{domainmcp.ErrBindingNotFound, 404},
		{domainmcp.ErrRemoteServerNotFound, 404},
		{domainmcp.ErrWorkspaceRequired, 400},
		{domainmcp.ErrServerKeyRequired, 400},
		{domainmcp.ErrURLRequired, 400},
		{domainmcp.ErrURLNotHTTPS, 400},
		{domainmcp.ErrUnknownAuthMode, 400},
		{domainmcp.ErrCredentialRequired, 400},
		{domainmcp.ErrNameRequired, 400},
		{domainmcp.ErrToolNameMalformed, 400},
		{domainmcp.ErrDuplicate, 409},
		{errors.New("misc"), 500},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.writeDomainError(w, c.err)
		if w.Code != c.code {
			t.Fatalf("err=%v got=%d want=%d", c.err, w.Code, c.code)
		}
	}
}

func TestRemoteRegister(t *testing.T) {
	e := newEnv(t)

	w := doJSON(t, e.remoteH.Register, "POST", "/", `{}`, "", nil)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.remoteH.Register, "POST", "/", `bad`, "ws", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.remoteH.Register, "POST", "/", `{"authMode":"weird"}`, "ws", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.remoteH.Register, "POST", "/", `{"name":"x","authMode":"none"}`, "ws", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	body := `{"name":"test","url":"https://example.test/mcp","authMode":"none"}`
	w = doJSON(t, e.remoteH.Register, "POST", "/", body, "ws", nil)
	if w.Code != 201 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRemoteList(t *testing.T) {
	e := newEnv(t)
	w := doJSON(t, e.remoteH.List, "GET", "/", "", "", nil)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = doJSON(t, e.remoteH.List, "GET", "/", "", "ws", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}

	s, _ := domainmcp.NewRemoteMCPServer("ls1", "ws", "srv", "https://srv.test", domainmcp.TransportStreamableHTTP)
	_ = e.remotes.Create(context.Background(), s)
	w = doJSON(t, e.remoteH.List, "GET", "/", "", "ws", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

type failingRemotes struct{}

func (failingRemotes) Create(context.Context, *domainmcp.RemoteMCPServer) error { return nil }
func (failingRemotes) Update(context.Context, *domainmcp.RemoteMCPServer) error { return nil }
func (failingRemotes) Get(context.Context, string, string) (*domainmcp.RemoteMCPServer, error) {
	return nil, errors.New("boom")
}
func (failingRemotes) ListByWorkspace(context.Context, string) ([]*domainmcp.RemoteMCPServer, error) {
	return nil, errors.New("boom")
}
func (failingRemotes) Delete(context.Context, string, string) error { return errors.New("boom") }

func TestRemoteListRepoError(t *testing.T) {
	h := NewAgentMCPRemoteHandler(nil, nil, failingRemotes{}, memrepo.NewToolCacheRepo())
	w := doJSON(t, h.List, "GET", "/", "", "ws", nil)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}

func TestRemoteGet(t *testing.T) {
	e := newEnv(t)
	w := doJSON(t, e.remoteH.Get, "GET", "/", "", "", map[string]string{"id": "x"})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = doJSON(t, e.remoteH.Get, "GET", "/", "", "ws", map[string]string{"id": "missing"})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}

	doJSON(t, e.remoteH.Register, "POST", "/", `{"name":"t","url":"https://x.test/mcp","authMode":"none"}`, "ws", nil)
	list, _ := e.remotes.ListByWorkspace(context.Background(), "ws")
	w = doJSON(t, e.remoteH.Get, "GET", "/", "", "ws", map[string]string{"id": list[0].ID})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestRemoteDelete(t *testing.T) {
	e := newEnv(t)
	w := doJSON(t, e.remoteH.Delete, "DELETE", "/", "", "", map[string]string{"id": "x"})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}

	w = doJSON(t, e.remoteH.Delete, "DELETE", "/", "", "ws", map[string]string{"id": "missing"})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}

	doJSON(t, e.remoteH.Register, "POST", "/", `{"name":"t","url":"https://x.test/mcp","authMode":"none"}`, "ws", nil)
	list, _ := e.remotes.ListByWorkspace(context.Background(), "ws")
	w = doJSON(t, e.remoteH.Delete, "DELETE", "/", "", "ws", map[string]string{"id": list[0].ID})
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

type failingCache struct{}

func (failingCache) Replace(context.Context, string, string, []domainmcp.CachedTool) error {
	return nil
}
func (failingCache) List(context.Context, string, string) ([]domainmcp.CachedTool, error) {
	return nil, nil
}
func (failingCache) Purge(context.Context, string, string) error { return errors.New("purge") }

func TestRemoteDeleteCachePurgeErr(t *testing.T) {
	h := NewAgentMCPRemoteHandler(nil, nil, memrepo.NewRemoteServerRepo(), failingCache{})
	w := doJSON(t, h.Delete, "DELETE", "/", "", "ws", map[string]string{"id": "x"})
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}

func TestOAuthCallback(t *testing.T) {
	e := newEnv(t)

	w0 := doJSON(t, e.h.Enable, "POST", "/", `{"serverKey":"gcal"}`, "ws", nil)
	var created map[string]any
	_ = json.Unmarshal(w0.Body.Bytes(), &created)
	gcalID, _ := created["id"].(string)

	signer := oauth.NewSigner(bytes.Repeat([]byte{7}, 32), 0)
	state, _ := signer.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: gcalID, Verifier: "v"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("code") == "bad" {
			http.Error(w, "nope", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ucmcp.TokenResponse{AccessToken: "tok", ExpiresIn: 3600, TokenType: "Bearer"})
	}))
	defer ts.Close()

	resolver := fixedResolver{cfg: ucmcp.OAuth2Config{
		AuthzURL: "https://a", TokenURL: ts.URL, ClientID: "cid", ClientSecret: "csec", RedirectURL: "https://r", UsePKCE: true,
	}}
	complete := &ucmcp.CompleteOAuth2UseCase{
		Resolver: resolver,
		Signer:   signer,
		Bindings: e.bindings,
		Remotes:  e.remotes,
		Vault:    e.vault,
	}
	cb := NewAgentMCPOAuthCallbackHandler(complete, "")

	r := httptest.NewRequest("GET", "/cb?error=access_denied", nil)
	w := httptest.NewRecorder()
	cb.Callback(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	r = httptest.NewRequest("GET", "/cb", nil)
	w = httptest.NewRecorder()
	cb.Callback(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	r = httptest.NewRequest("GET", "/cb?code=c&state=bad", nil)
	w = httptest.NewRecorder()
	cb.Callback(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}

	r = httptest.NewRequest("GET", "/cb?code=good&state="+state, nil)
	w = httptest.NewRecorder()
	cb.Callback(w, r)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}

	cb2 := NewAgentMCPOAuthCallbackHandler(complete, "/done")
	r = httptest.NewRequest("GET", "/cb?code=good&state="+state, nil)
	w = httptest.NewRecorder()
	cb2.Callback(w, r)
	if w.Code != 302 {
		t.Fatal(w.Code)
	}

	state2, _ := signer.Sign(oauth.State{Kind: "builtin", WorkspaceID: "ws", BindingID: gcalID, Verifier: "v"})
	r = httptest.NewRequest("GET", "/cb?code=bad&state="+state2, nil)
	w = httptest.NewRecorder()
	cb.Callback(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

type fixedResolver struct{ cfg ucmcp.OAuth2Config }

func (f fixedResolver) Resolve(context.Context, string, string, string) (ucmcp.OAuth2Config, error) {
	return f.cfg, nil
}

func TestWriteJSONDiscard(t *testing.T) {
	var buf bytes.Buffer
	writeJSON(&discardRW{h: http.Header{}, buf: &buf}, 200, map[string]string{"a": "b"})
	if !strings.Contains(buf.String(), "a") {
		t.Fatal(buf.String())
	}
}

type discardRW struct {
	h   http.Header
	buf *bytes.Buffer
}

func (d *discardRW) Header() http.Header         { return d.h }
func (d *discardRW) Write(p []byte) (int, error) { return d.buf.Write(p) }
func (d *discardRW) WriteHeader(int)             {}

var _ io.Writer = (*discardRW)(nil)
