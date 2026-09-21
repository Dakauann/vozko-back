package node_executors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/workflow"
)

func allowLoopback(t *testing.T) {
	t.Helper()
	saved := privateIPNets
	privateIPNets = nil
	t.Cleanup(func() { privateIPNets = saved })
}

func httpNodeCtx(id string, config map[string]interface{}, state *workflow.RunState) *workflow.NodeContext {
	return &workflow.NodeContext{
		Node: &workflow.Node{ID: id, Config: config},
		Graph: &workflow.Graph{Edges: []workflow.Edge{
			{Source: id, Target: "ok", Label: "sucesso"},
			{Source: id, Target: "err", Label: "falha"},
		}},
		State: state,
	}
}

func TestHTTPExecutor_ArrayBodyCaptured_IsNavigable(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"token":"3656101BC0"},{"id":2,"token":"SECOND"}]`))
	}))
	defer srv.Close()

	state := workflow.NewRunState()
	ctx := httpNodeCtx("s2_1", map[string]interface{}{
		"url":              srv.URL,
		"method":           "GET",
		"capture_variable": "token_consulta_cadastro",
	}, &state)

	if _, err := NewHTTPRequestExecutor().Execute(ctx); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := workflow.Interpolate("{{token_consulta_cadastro[0].token}}", &state, nil); got != "3656101BC0" {
		t.Fatalf("array capture not navigable: got %q, want 3656101BC0", got)
	}
	if got := workflow.Interpolate("{{token_consulta_cadastro[1].id}}", &state, nil); got != "2" {
		t.Fatalf("second element id: got %q, want 2", got)
	}
}

func TestHTTPExecutor_TokenChain_SendsSingleCorrectBearer(t *testing.T) {
	allowLoopback(t)
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"token":"REALTOKEN"}]`))
	}))
	defer tokenSrv.Close()

	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth == "Bearer REALTOKEN" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer apiSrv.Close()

	state := workflow.NewRunState()
	exec := NewHTTPRequestExecutor()

	ctx1 := httpNodeCtx("s2_1", map[string]interface{}{
		"url": tokenSrv.URL, "method": "GET", "capture_variable": "token_consulta_cadastro",
	}, &state)
	if _, err := exec.Execute(ctx1); err != nil {
		t.Fatalf("s2_1: %v", err)
	}

	ctx2 := httpNodeCtx("s2_2", map[string]interface{}{
		"url":              apiSrv.URL,
		"method":           "GET",
		"auth_type":        "bearer",
		"auth_token":       "{{token_consulta_cadastro[0].token}}",
		"capture_variable": "dados_beneficiario",
	}, &state)
	res2, err := exec.Execute(ctx2)
	if err != nil {
		t.Fatalf("s2_2: %v", err)
	}

	if gotAuth != "Bearer REALTOKEN" {
		t.Fatalf("downstream Authorization = %q, want \"Bearer REALTOKEN\"", gotAuth)
	}
	if res2.Output["status_code"] != http.StatusOK {
		t.Fatalf("status_code = %v, want 200", res2.Output["status_code"])
	}
}

func TestHTTPExecutor_StripsDuplicateBearerPrefix(t *testing.T) {
	allowLoopback(t)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	state := workflow.NewRunState()
	state.Set("token_consulta_cadastro", []interface{}{
		map[string]interface{}{"token": "REALTOKEN"},
	})
	ctx := httpNodeCtx("s2_2", map[string]interface{}{
		"url":        srv.URL,
		"method":     "GET",
		"auth_type":  "bearer",
		"auth_token": "Bearer {{token_consulta_cadastro[0].token}}",
	}, &state)
	if _, err := NewHTTPRequestExecutor().Execute(ctx); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotAuth != "Bearer REALTOKEN" {
		t.Fatalf("duplicate Bearer not stripped: Authorization = %q", gotAuth)
	}
}

func TestHTTPExecutor_StatusCodeOnCaptureVar(t *testing.T) {
	allowLoopback(t)

	t.Run("object body (401) exposes status_code and keeps body fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Authorization failed"}`))
		}))
		defer srv.Close()

		state := workflow.NewRunState()
		ctx := httpNodeCtx("s2_2", map[string]interface{}{
			"url": srv.URL, "method": "GET", "capture_variable": "dados_beneficiario",
		}, &state)
		if _, err := NewHTTPRequestExecutor().Execute(ctx); err != nil {
			t.Fatalf("execute: %v", err)
		}

		if got := workflow.Interpolate("{{dados_beneficiario.status_code}}", &state, nil); got != "401" {
			t.Fatalf("{{dados_beneficiario.status_code}} = %q, want 401", got)
		}
		if got := workflow.Interpolate("{{dados_beneficiario.success}}", &state, nil); got != "false" {
			t.Fatalf("{{dados_beneficiario.success}} = %q, want false", got)
		}
		if got := workflow.Interpolate("{{dados_beneficiario.error}}", &state, nil); got != "Authorization failed" {
			t.Fatalf("{{dados_beneficiario.error}} = %q, want Authorization failed", got)
		}
	})

	t.Run("array body exposes status_code too", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"token":"A"}]`))
		}))
		defer srv.Close()

		state := workflow.NewRunState()
		ctx := httpNodeCtx("s2_1", map[string]interface{}{
			"url": srv.URL, "method": "GET", "capture_variable": "token_consulta_cadastro",
		}, &state)
		if _, err := NewHTTPRequestExecutor().Execute(ctx); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if got := workflow.Interpolate("{{token_consulta_cadastro.status_code}}", &state, nil); got != "200" {
			t.Fatalf("array capture status_code = %q, want 200", got)
		}
		if got := workflow.Interpolate("{{token_consulta_cadastro[0].token}}", &state, nil); got != "A" {
			t.Fatalf("array index broke: %q", got)
		}
	})
}
