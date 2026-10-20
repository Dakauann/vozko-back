package node_executors

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/redact"
	"vozko/domain/workflow"
)

type fakeSecretResolver struct {
	values map[string]string
	calls  [][]string
	err    error
}

func (f *fakeSecretResolver) Resolve(_ context.Context, workspaceID string, names []string) (map[string]string, error) {
	f.calls = append(f.calls, append([]string{workspaceID}, names...))
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for _, n := range names {
		if v, ok := f.values[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

func secretNodeCtx(id string, config map[string]interface{}, state *workflow.RunState) *workflow.NodeContext {
	ctx := httpNodeCtx(id, config, state)
	ctx.Run = &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1"}
	return ctx
}

func captureServerLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prevSlog := slog.Default()
	prevLog := log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	log.SetOutput(buf)
	t.Cleanup(func() {
		slog.SetDefault(prevSlog)
		log.SetOutput(prevLog)
	})
	return buf
}

func stateText(state *workflow.RunState) string {
	raw, _ := state.JSON()
	return string(raw)
}

func TestHTTPNodeSendsSecretsButNeverKeepsThemInOutputStateOrLogs(t *testing.T) {
	var gotAuth, gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.Query().Get("k")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"echo":"` + gotBody + `","accessToken":"tok-from-api"}`))
	}))
	defer srv.Close()
	logs := captureServerLogs(t)

	resolver := &fakeSecretResolver{values: map[string]string{"api_key": "S3CR3T-KEY", "senha": "p4ss-w0rd"}}
	state := workflow.NewRunState()
	state.Set("login", "ana")
	ctx := secretNodeCtx("n55", map[string]interface{}{
		"method":           "POST",
		"url":              srv.URL + "/login?k={{secret.api_key}}",
		"headers":          map[string]interface{}{"Authorization": "Bearer {{secret.api_key}}"},
		"body":             `{{login}}:{{secret.senha}}`,
		"capture_variable": "login_resp",
	}, &state)

	result, err := NewHTTPRequestExecutor(testEgress(), resolver).Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer S3CR3T-KEY" || gotQuery != "S3CR3T-KEY" || gotBody != "ana:p4ss-w0rd" {
		t.Fatalf("request did not carry the secrets: auth=%q query=%q body=%q", gotAuth, gotQuery, gotBody)
	}
	if !reflect.DeepEqual(resolver.calls, [][]string{{"ws-1", "api_key", "senha"}}) {
		t.Fatalf("resolve once per execution with every name: %v", resolver.calls)
	}
	workflow.RecordNodeOutput(&state, "n55", result.Output)
	for _, secret := range []string{"S3CR3T-KEY", "p4ss-w0rd"} {
		if strings.Contains(stateText(&state), secret) {
			t.Fatalf("secret %q reached the run state: %s", secret, stateText(&state))
		}
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("secret %q reached the server log: %s", secret, logs.String())
		}
	}
	if result.Output["accessToken"] != "tok-from-api" {
		t.Fatalf("the run keeps real values it needs: %v", result.Output["accessToken"])
	}
	if !strings.Contains(stateText(&state), redact.Mask) {
		t.Fatal("the echoed secret must be masked in place")
	}
}

func TestHTTPNodeServerLogCarriesOnlyMethodHostPathAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"cpf":"52998224725","nome":"Ana"}`))
	}))
	defer srv.Close()
	logs := captureServerLogs(t)

	state := workflow.NewRunState()
	ctx := secretNodeCtx("n7", map[string]interface{}{"method": "GET", "url": srv.URL + "/alunos?cpf=52998224725"}, &state)
	if _, err := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	for _, banned := range []string{"52998224725", "Ana", "cpf="} {
		if strings.Contains(out, banned) {
			t.Fatalf("server log leaked %q: %s", banned, out)
		}
	}
	for _, want := range []string{"method=GET", "path=/alunos", "status=200", "run_id=run-1", "node_id=n7", "workspace_id=ws-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("server log misses %q: %s", want, out)
		}
	}
}

func TestHTTPNodeFailsClosedNamingAMissingSecretAndSendsNothing(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()

	cases := []struct {
		name     string
		resolver *fakeSecretResolver
	}{
		{"secret not created", &fakeSecretResolver{values: map[string]string{"other": "x"}}},
		{"no resolver on this server", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := workflow.NewRunState()
			ctx := secretNodeCtx("n55", map[string]interface{}{"url": srv.URL + "/{{secret.unifecaf_senha}}"}, &state)
			var exec workflow.NodeExecutor
			if c.resolver == nil {
				exec = NewHTTPRequestExecutor(testEgress(), nil)
			} else {
				exec = NewHTTPRequestExecutor(testEgress(), c.resolver)
			}
			result, err := exec.Execute(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Error, "unifecaf_senha") {
				t.Fatalf("error must name the secret: %q", result.Error)
			}
			if hits != 0 {
				t.Fatal("no request may leave with a literal placeholder")
			}
		})
	}
}

func TestHTTPNodeRetriesWhenTheSecretStoreIsUnreadable(t *testing.T) {
	state := workflow.NewRunState()
	ctx := secretNodeCtx("n55", map[string]interface{}{"url": "https://x.test/{{secret.k}}"}, &state)
	_, err := NewHTTPRequestExecutor(testEgress(), &fakeSecretResolver{err: errors.New("db down")}).Execute(ctx)
	if err == nil {
		t.Fatal("a store failure must surface as an error so the engine retries")
	}
}

func TestHTTPNodeWithoutSecretReferencesNeverQueriesTheStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	resolver := &fakeSecretResolver{}
	state := workflow.NewRunState()
	if _, err := NewHTTPRequestExecutor(testEgress(), resolver).Execute(secretNodeCtx("n1", map[string]interface{}{"url": srv.URL}, &state)); err != nil {
		t.Fatal(err)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("unexpected queries: %v", resolver.calls)
	}
}

func TestHTTPNodeTransportErrorDoesNotRevealTheSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	logs := captureServerLogs(t)

	state := workflow.NewRunState()
	ctx := secretNodeCtx("n55", map[string]interface{}{"url": url + "/x?k={{secret.k}}"}, &state)
	result, err := NewHTTPRequestExecutor(testEgress(), &fakeSecretResolver{values: map[string]string{"k": "TOPSECRET"}}).Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output["error"].(string), "TOPSECRET") || strings.Contains(logs.String(), "TOPSECRET") {
		t.Fatalf("transport error leaked the secret: %v / %s", result.Output["error"], logs.String())
	}
}

func TestCodeNodeGetsOnlyTheSecretsItNamesAndNeverStoresTheirValues(t *testing.T) {
	resolver := &fakeSecretResolver{values: map[string]string{"unifecaf_login": "ana", "unifecaf_senha": "p4ss-w0rd", "unused": "x"}}
	state := workflow.NewRunState()
	code := `const senha = secrets.get('unifecaf_senha');
state.set('copia', 'pre-' + senha);
return { ok: secrets.get("unifecaf_login") === 'ana' && senha === 'p4ss-w0rd', leak: senha };`
	ctx := secretNodeCtx("n56", map[string]interface{}{"code": code}, &state)

	result, err := NewCodeExecutor(resolver).Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolver.calls, [][]string{{"ws-1", "unifecaf_senha", "unifecaf_login"}}) {
		t.Fatalf("resolve once with the literal names only: %v", resolver.calls)
	}
	if result.Output["ok"] != true {
		t.Fatalf("the script must see the real values: %v", result.Output)
	}
	if result.Output["leak"] != redact.Mask {
		t.Fatalf("a returned secret must be masked: %v", result.Output["leak"])
	}
	if got, _ := state.Get("copia"); got != "pre-"+redact.Mask {
		t.Fatalf("a secret written to state must be masked: %v", got)
	}
}

func TestCodeNodeNamesASecretThatDoesNotExist(t *testing.T) {
	state := workflow.NewRunState()
	ctx := secretNodeCtx("n56", map[string]interface{}{"code": `return { v: secrets.get('absent_key') };`}, &state)
	_, err := NewCodeExecutor(&fakeSecretResolver{}).Execute(ctx)
	if err == nil || !strings.Contains(err.Error(), "absent_key") {
		t.Fatalf("error must name the secret: %v", err)
	}
}

func TestCodeNodeWithoutSecretsNeverQueriesTheStore(t *testing.T) {
	resolver := &fakeSecretResolver{}
	state := workflow.NewRunState()
	if _, err := NewCodeExecutor(resolver).Execute(secretNodeCtx("n56", map[string]interface{}{"code": `return { a: 1 };`}, &state)); err != nil {
		t.Fatal(err)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("unexpected queries: %v", resolver.calls)
	}
}
