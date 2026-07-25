package scriptvm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type mapState struct {
	mu  sync.Mutex
	bag map[string]interface{}
}

func newMapState() *mapState { return &mapState{bag: map[string]interface{}{}} }

func (m *mapState) Get(k string) (interface{}, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.bag[k]
	return v, ok
}
func (m *mapState) Set(k string, v interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bag[k] = v
}

func TestRun_ReturnsObject(t *testing.T) {
	s := New(DefaultLimits())
	res, err := s.Run(context.Background(), `return { greeting: "hello " + input.name };`, Capabilities{
		Input: map[string]interface{}{"name": "world"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := res.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Value)
	}
	if m["greeting"] != "hello world" {
		t.Fatalf("unexpected greeting: %v", m["greeting"])
	}
}

func TestRun_TimeoutKillsInfiniteLoop(t *testing.T) {
	s := New(Limits{Wallclock: 200 * time.Millisecond})
	start := time.Now()
	_, err := s.Run(context.Background(), `while(true){}`, Capabilities{})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout enforcement too slow: %v", elapsed)
	}
}

func TestRun_NoEvalNoFunctionCtor(t *testing.T) {
	s := New(DefaultLimits())
	for _, code := range []string{
		`return eval("1+1");`,
		`return (new Function("return 2+2"))();`,
		`return require("fs");`,
	} {
		if _, err := s.Run(context.Background(), code, Capabilities{}); err == nil {
			t.Fatalf("expected rejection for %q", code)
		}
	}
}

func TestRun_NoTimersOrWorkers(t *testing.T) {
	s := New(DefaultLimits())

	res, err := s.Run(context.Background(), `
		var names = ["setTimeout", "setInterval", "clearTimeout", "Wo" + "rker", "Pro" + "mise"];
		return names.map(function(n){ return typeof globalThis[n]; }).join(",");
	`, Capabilities{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := res.Value.(string)
	if got != "undefined,undefined,undefined,undefined,undefined" {
		t.Fatalf("expected all undefined, got %q", got)
	}
}

func TestRun_StateGetSet(t *testing.T) {
	s := New(DefaultLimits())
	st := newMapState()
	st.Set("counter", float64(1))
	res, err := s.Run(context.Background(), `
		var c = state.get("counter");
		state.set("counter", c + 1);
		return { previous: c };
	`, Capabilities{State: st})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, _ := st.Get("counter")
	if asFloat(v) != 2 {
		t.Fatalf("expected counter==2, got %v (%T)", v, v)
	}
	if res.Audit.StateWrites[0] != "counter" {
		t.Fatalf("expected state write audit, got %v", res.Audit.StateWrites)
	}
}

func TestRun_StateGetDottedPathTraversal(t *testing.T) {

	s := New(DefaultLimits())
	st := newMapState()
	st.Set("ai_response", map[string]interface{}{
		"response_text": "ok",
		"tool_name":     "pesquisar_na_internet",
		"tool_args": map[string]interface{}{
			"query":    "eleição 2026",
			"language": "pt",
		},
	})
	res, err := s.Run(context.Background(), `
		var q = state.get("ai_response.tool_args.query");
		var lang = state.get("ai_response.tool_args.language");
		var missing = state.get("ai_response.tool_args.missing");
		var deep = state.get("ai_response.tool_args.query.notObject");
		return { q: q, lang: lang, missing: typeof missing, deep: typeof deep };
	`, Capabilities{State: st})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.Value.(map[string]interface{})
	if m["q"] != "eleição 2026" {
		t.Fatalf("expected dotted-path traversal to return query, got %v", m["q"])
	}
	if m["lang"] != "pt" {
		t.Fatalf("expected dotted-path traversal to return language, got %v", m["lang"])
	}
	if m["missing"] != "undefined" {
		t.Fatalf("expected missing leaf to be undefined, got %v", m["missing"])
	}
	if m["deep"] != "undefined" {
		t.Fatalf("expected traversal past non-object to be undefined, got %v", m["deep"])
	}
}

func TestRun_StateGetFlatKeyTakesPrecedenceOverDottedPath(t *testing.T) {

	s := New(DefaultLimits())
	st := newMapState()
	st.Set("ai_response.tool_args.cep", "01001-000")
	st.Set("ai_response", map[string]interface{}{
		"tool_args": map[string]interface{}{"cep": "should-not-win"},
	})
	res, err := s.Run(context.Background(), `
		return { v: state.get("ai_response.tool_args.cep") };
	`, Capabilities{State: st})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.Value.(map[string]interface{})
	if m["v"] != "01001-000" {
		t.Fatalf("expected flat key to win, got %v", m["v"])
	}
}

func TestRun_StateGetArrayIndexInPath(t *testing.T) {
	s := New(DefaultLimits())
	st := newMapState()
	st.Set("results", map[string]interface{}{
		"hits": []interface{}{
			map[string]interface{}{"title": "first"},
			map[string]interface{}{"title": "second"},
		},
	})
	res, err := s.Run(context.Background(), `
		return {
			first:  state.get("results.hits.0.title"),
			second: state.get("results.hits.1.title"),
			oob:    typeof state.get("results.hits.5.title"),
		};
	`, Capabilities{State: st})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.Value.(map[string]interface{})
	if m["first"] != "first" || m["second"] != "second" || m["oob"] != "undefined" {
		t.Fatalf("unexpected array traversal result: %#v", m)
	}
}

func TestRun_StateGetMissingTopLevelKey(t *testing.T) {
	s := New(DefaultLimits())
	st := newMapState()
	res, err := s.Run(context.Background(), `
		return { v: typeof state.get("ai_response.tool_args.query") };
	`, Capabilities{State: st})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.Value.(map[string]interface{})
	if m["v"] != "undefined" {
		t.Fatalf("expected undefined for missing top-level key, got %v", m["v"])
	}
}

func TestRun_SecretsNotEnumerable(t *testing.T) {
	s := New(DefaultLimits())
	res, err := s.Run(context.Background(), `
		return { keys: Object.keys(secrets), value: secrets.get("api_key") };
	`, Capabilities{Secrets: map[string]string{"api_key": "sk_test_123"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.Value.(map[string]interface{})
	keys, _ := m["keys"].([]interface{})

	if len(keys) != 1 || keys[0] != "get" {
		t.Fatalf("expected only ['get'] on secrets, got %v", keys)
	}
	if m["value"] != "sk_test_123" {
		t.Fatalf("expected secret value, got %v", m["value"])
	}
}

func TestRun_SecretsMissingThrows(t *testing.T) {
	s := New(DefaultLimits())
	_, err := s.Run(context.Background(), `return secrets.get("missing");`, Capabilities{})
	if err == nil {
		t.Fatalf("expected error for missing secret")
	}
}

func TestRun_OutputSizeCap(t *testing.T) {
	s := New(Limits{Wallclock: 5 * time.Second, MaxOutputBytes: 1024})
	_, err := s.Run(context.Background(), `
		var s = ""; for (var i=0; i<200; i++) s += "0123456789";
		return { data: s };
	`, Capabilities{})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("expected ErrOutputTooLarge, got %v", err)
	}
}

func TestRun_FetchBlocksLoopback(t *testing.T) {
	s := New(Limits{Wallclock: 3 * time.Second, AllowFetch: true, MaxFetchCalls: 5, MaxFetchBodyBytes: 1024})
	_, err := s.Run(context.Background(), `return fetch("http://127.0.0.1:80/");`, Capabilities{})
	if err == nil {
		t.Fatalf("expected fetch rejection")
	}
}

func TestRun_FetchBlocksMetadata(t *testing.T) {
	s := New(Limits{Wallclock: 3 * time.Second, AllowFetch: true, AllowHTTPInsecure: true, MaxFetchCalls: 5, MaxFetchBodyBytes: 1024})
	_, err := s.Run(context.Background(), `return fetch("http://169.254.169.254/latest/meta-data/");`, Capabilities{})
	if err == nil {
		t.Fatalf("expected metadata IP to be blocked")
	}
	if !strings.Contains(err.Error(), "169.254") && !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("error should mention blocked address: %v", err)
	}
}

func TestRun_FetchHTTPSchemeBlockedByDefault(t *testing.T) {
	s := New(Limits{Wallclock: 3 * time.Second, AllowFetch: true, MaxFetchCalls: 5, MaxFetchBodyBytes: 1024})
	_, err := s.Run(context.Background(), `return fetch("http://example.com/");`, Capabilities{})
	if err == nil {
		t.Fatalf("expected http:// to be rejected when AllowHTTPInsecure=false")
	}
}

func TestRun_FetchAllowlistRejectsHost(t *testing.T) {
	s := New(Limits{
		Wallclock:         3 * time.Second,
		AllowFetch:        true,
		MaxFetchCalls:     5,
		MaxFetchBodyBytes: 1024,
		EgressAllowlist:   []string{"api.stripe.com"},
	})
	_, err := s.Run(context.Background(), `return fetch("https://example.com/");`, Capabilities{})
	if err == nil {
		t.Fatalf("expected host-not-in-allowlist rejection")
	}
}

func TestRun_StaticBanCatchesEval(t *testing.T) {
	if err := PreCheck(`var x = eval("1+1");`); !errors.Is(err, ErrStaticReject) {
		t.Fatalf("expected ErrStaticReject, got %v", err)
	}
	if err := PreCheck(`return 1+1;`); err != nil {
		t.Fatalf("benign code rejected: %v", err)
	}
}

func TestRun_LogBudget(t *testing.T) {
	s := New(Limits{Wallclock: 2 * time.Second, MaxLogBytes: 32})
	_, err := s.Run(context.Background(), `
		log("info", "1234567890");
		log("info", "1234567890");
		log("info", "1234567890");
		log("info", "1234567890");
		return {};
	`, Capabilities{})
	if err == nil {
		t.Fatalf("expected log budget error")
	}
}

func asFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

func TestRun_ParallelIsolation(t *testing.T) {
	s := New(DefaultLimits())
	const N = 30
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := s.Run(context.Background(), `return { n: input.n * 2 };`, Capabilities{
				Input: map[string]interface{}{"n": i},
			})
			if err != nil {
				errs <- err
				return
			}
			m := res.Value.(map[string]interface{})
			if int(asFloat(m["n"])) != i*2 {
				errs <- errors.New("wrong result")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("parallel run failed: %v", e)
	}
}

func TestRun_DisabledByKillSwitch(t *testing.T) {
	t.Setenv("WORKFLOW_SCRIPT_DISABLED", "1")
	s := New(DefaultLimits())
	_, err := s.Run(context.Background(), `return 1;`, Capabilities{})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}
