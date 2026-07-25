package tools_usecase

import "testing"

func TestHTTPRequestTool_DefinitionConfigSchemaMetadata(t *testing.T) {
	tool := NewHTTPRequestToolUseCase()
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
