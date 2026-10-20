package node_executors

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func tracedHTTPCtx(id string, config map[string]interface{}) (*workflow.NodeContext, *workflow.StepTrace) {
	state := workflow.NewRunState()
	ctx := secretNodeCtx(id, config, &state)
	ctx.Trace = &workflow.StepTrace{}
	return ctx, ctx.Trace
}

func scrubbedForStorage(trace *workflow.StepTrace, x workflow.HTTPExchange) workflow.HTTPExchange {
	for _, r := range trace.Scrubbers() {
		x = x.Redact(r)
	}
	return x
}

func TestTheHTTPNodeRecordsTheWholeExchangeForTheStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("X-Request-Id", "req-9")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"accessToken":"tok-1"}`))
	}))
	defer srv.Close()

	resolver := &fakeSecretResolver{values: map[string]string{"api_key": "TOPSECRET"}}
	ctx, trace := tracedHTTPCtx("n55", map[string]interface{}{
		"method":       "GET",
		"url":          srv.URL + "/old?curso=ADS",
		"headers":      map[string]interface{}{"X-Curso": "ADS"},
		"auth_type":    "bearer",
		"auth_token":   "{{secret.api_key}}",
		"query_params": map[string]interface{}{"token": "q-1"},
	})

	if _, err := NewHTTPRequestExecutor(testEgress(), resolver).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	exchanges := trace.Exchanges()
	if len(exchanges) != 1 {
		t.Fatalf("exchanges = %+v", exchanges)
	}
	x := scrubbedForStorage(trace, exchanges[0])
	if x.Method != "GET" || x.Status != http.StatusCreated || x.Redirects != 1 || !strings.HasSuffix(strings.Split(x.URL, "?")[0], "/login") {
		t.Fatalf("exchange = %+v", x)
	}
	if x.RequestHeaders["X-Curso"] != "ADS" || x.ResponseHeaders["X-Request-Id"] != "req-9" {
		t.Fatalf("headers = %v / %v", x.RequestHeaders, x.ResponseHeaders)
	}
	if strings.Contains(x.RequestHeaders["Authorization"], "TOPSECRET") {
		t.Fatalf("the secret value reached the stored exchange: %v", x.RequestHeaders)
	}
	if x.ResponseBody != `{"accessToken":"tok-1"}` || x.LatencyMs < 0 || x.TLSFallback {
		t.Fatalf("exchange = %+v", x)
	}
}

func TestAnHTTPFailureBranchSaysWhyTheNodeTookIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, trace := tracedHTTPCtx("n55", map[string]interface{}{"url": srv.URL})
	result, err := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorKind != workflow.ErrorKindHTTPStatus || result.NextNodeID != "err" {
		t.Fatalf("result = %+v", result)
	}
	if trace.Exchanges()[0].Status != http.StatusInternalServerError {
		t.Fatalf("exchange = %+v", trace.Exchanges()[0])
	}
}

type refusingTransport struct{}

func (refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("dial tcp: %w: 10.0.0.5", shared.ErrEgressBlocked)
}

func TestAnEgressRefusalIsRecordedWithItsReason(t *testing.T) {
	ctx, trace := tracedHTTPCtx("n55", map[string]interface{}{"url": "http://intranet.local/x"})
	result, err := NewHTTPRequestExecutor(HTTPEgress{Client: &http.Client{Transport: refusingTransport{}}}, nil).Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorKind != workflow.ErrorKindTransport {
		t.Fatalf("kind = %q", result.ErrorKind)
	}
	x := trace.Exchanges()[0]
	if !strings.Contains(x.EgressRefusal, "10.0.0.5") || x.Status != 0 {
		t.Fatalf("exchange = %+v", x)
	}
}

func TestAnHTTPNodeWithoutAURLIsAConfigFailure(t *testing.T) {
	ctx, _ := tracedHTTPCtx("n55", map[string]interface{}{"url": ""})
	result, _ := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx)
	if result.ErrorKind != workflow.ErrorKindConfig {
		t.Fatalf("kind = %q", result.ErrorKind)
	}
}

func TestALargeResponseIsMarkedTruncatedInTheExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxHTTPResponseBytes+100)))
	}))
	defer srv.Close()
	ctx, trace := tracedHTTPCtx("n55", map[string]interface{}{"url": srv.URL})
	result, err := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := result.Output["body"].(string); len(body) != maxHTTPResponseBytes {
		t.Fatalf("the node output keeps its existing 64 KB body, got %d bytes", len(body))
	}
	if x := trace.Exchanges()[0]; !x.Truncated || len(x.ResponseBody) != maxHTTPResponseBytes {
		t.Fatalf("exchange truncated=%v body=%d", x.Truncated, len(x.ResponseBody))
	}
}

func tracedCodeCtx(code string) (*workflow.NodeContext, *workflow.StepTrace) {
	state := workflow.NewRunState()
	ctx := secretNodeCtx("n56", map[string]interface{}{"code": code}, &state)
	ctx.Trace = &workflow.StepTrace{}
	return ctx, ctx.Trace
}

func TestTheCodeNodeKeepsItsLogCallsOnTheStep(t *testing.T) {
	ctx, trace := tracedCodeCtx(`log('info', 'etapa', {motivo: 'LOGIN'}); return {ok: true};`)
	if _, err := NewCodeExecutor(nil).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	logs := trace.Logs()
	if len(logs) != 1 || logs[0].Level != "info" || !strings.Contains(logs[0].Message, "LOGIN") {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestAScriptErrorKeepsTheLogsBeforeItAndTheLineInsideTheNodeCode(t *testing.T) {
	ctx, trace := tracedCodeCtx("log('info', 'antes');\nconst r = null;\nreturn r.status;")
	_, err := NewCodeExecutor(nil).Execute(ctx)
	if workflow.ClassifyError(err) != workflow.ErrorKindScript {
		t.Fatalf("err = %v, kind = %q", err, workflow.ClassifyError(err))
	}
	if len(trace.Logs()) != 1 || trace.Logs()[0].Message != "antes" {
		t.Fatalf("logs = %+v", trace.Logs())
	}
	if !strings.Contains(trace.StackText(), "(3:") {
		t.Fatalf("stack = %q, want the failing line 3 of the node code", trace.StackText())
	}
}

func TestAScriptTimeoutIsATimeout(t *testing.T) {
	ctx, _ := tracedCodeCtx("while (true) {}")
	ctx.Node.Config["timeout_ms"] = float64(50)
	_, err := NewCodeExecutor(nil).Execute(ctx)
	if kind := workflow.ClassifyError(err); kind != workflow.ErrorKindTimeout {
		t.Fatalf("err = %v, kind = %q", err, kind)
	}
}

func TestABlockedFetchIsRecordedAsAnExchangeWithTheRefusal(t *testing.T) {
	ctx, trace := tracedCodeCtx(`return fetch('https://127.0.0.1/admin?token=abc');`)
	_, err := NewCodeExecutor(nil).Execute(ctx)
	if err == nil {
		t.Fatal("a blocked fetch must fail the script")
	}
	exchanges := trace.Exchanges()
	if len(exchanges) != 1 || exchanges[0].EgressRefusal == "" || exchanges[0].Method != "GET" {
		t.Fatalf("exchanges = %+v", exchanges)
	}
}
