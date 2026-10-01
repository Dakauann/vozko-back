package tools_usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestHTTPRequestTool_DefinitionConfigSchemaMetadata(t *testing.T) {
	tool := NewHTTPRequestToolUseCase(http.DefaultClient)
	def := tool.Definition()

	if !def.RequiresConfig {
		t.Fatal("expected http_request tool to require agent-level config")
	}
	if got := def.ConfigSchema["url"].DisplayName; got != "URL do endpoint" {
		t.Fatalf("expected professional url label, got %q", got)
	}
	method := def.ConfigSchema["method"]
	if got := method.Default; got != "GET" {
		t.Fatalf("expected method default GET, got %v", got)
	}
	if len(method.Options) != 5 || method.Options[0].Value != "GET" || method.Options[4].Value != "DELETE" {
		t.Fatalf("expected HTTP method options, got %+v", method.Options)
	}
	if got := def.ConfigSchema["timeout_seconds"].Default; got != 30 {
		t.Fatalf("expected 30 second timeout default, got %v", got)
	}
	if got := def.ConfigSchema["headers"].DisplayName; got != "Cabeçalhos fixos" {
		t.Fatalf("expected professional headers label, got %q", got)
	}
	if err := def.ValidateConfig(map[string]interface{}{
		"url":             "https://api.example.com/leads/{lead_id}",
		"method":          def.ConfigSchema["method"].Default,
		"headers":         def.ConfigSchema["headers"].Default,
		"timeout_seconds": def.ConfigSchema["timeout_seconds"].Default,
		"path_params":     def.ConfigSchema["path_params"].Default,
		"query_schema":    def.ConfigSchema["query_schema"].Default,
		"body_schema":     def.ConfigSchema["body_schema"].Default,
	}); err != nil {
		t.Fatalf("expected schema defaults to be valid config: %v", err)
	}
}

type recordingTransport struct{ requested string }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requested = req.URL.String()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}, Request: req}, nil
}

func TestHTTPRequestTool_ValuesFromTheConversationCannotReshapeTheURL(t *testing.T) {
	transport := &recordingTransport{}
	tool := NewHTTPRequestToolUseCase(&http.Client{Transport: transport}).(*httpRequestTool)
	_, err := tool.ExecuteWithConfig(context.Background(),
		map[string]interface{}{"url": "https://api.example.com/leads/{lead_id}", "method": "GET"},
		map[string]interface{}{
			"path_values":  map[string]interface{}{"lead_id": "../admin?x=1"},
			"query_values": map[string]interface{}{"q": "a&role=admin"},
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if transport.requested != "https://api.example.com/leads/..%2Fadmin%3Fx=1?q=a%26role%3Dadmin" {
		t.Fatalf("requested %q", transport.requested)
	}
}

func TestHTTPRequestTool_TheGuardedClientStopsInternalAddresses(t *testing.T) {
	refused := errors.New("refused by the guard")
	tool := NewHTTPRequestToolUseCase(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, refused })}).(*httpRequestTool)
	res, err := tool.ExecuteWithConfig(context.Background(),
		map[string]interface{}{"url": "http://169.254.169.254/latest/meta-data", "method": "GET"}, nil)
	if !errors.Is(err, refused) || !res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
