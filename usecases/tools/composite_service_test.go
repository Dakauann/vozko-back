package tools_usecase

import (
	"strings"
	"testing"

	"vozko/domain/tools"
)

func TestTruncateText(t *testing.T) {
	short := "hello world"
	if got := truncateText(short, 100); got != short {
		t.Errorf("short text should not be truncated, got %q", got)
	}
	long := strings.Repeat("a", 20000)
	got := truncateText(long, mcpMaxResultChars)
	if len(got) > mcpMaxResultChars+100 {
		t.Errorf("truncated text too long: %d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncated text should contain truncation notice")
	}
	if got[:mcpMaxResultChars] != long[:mcpMaxResultChars] {
		t.Error("truncated text prefix should match original")
	}
}

func TestParseSchemaToParams_Empty(t *testing.T) {
	params, required := parseSchemaToParams(nil)
	if params != nil {
		t.Fatalf("expected nil params, got %v", params)
	}
	if required != nil {
		t.Fatalf("expected nil required, got %v", required)
	}
}

func TestParseSchemaToParams_DefaultsEmptyTypeToString(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL to scrape"},
			"question": {"description": "No type field"},
			"maxResults": {"type": "number", "description": "Max results"}
		},
		"required": ["url"]
	}`)
	params, required := parseSchemaToParams(schema)
	if len(params) != 3 {
		t.Fatalf("expected 3 params, got %d: %+v", len(params), params)
	}
	if params["url"].Type != "string" {
		t.Errorf("url type = %q, want string", params["url"].Type)
	}
	if params["url"].Description != "The URL to scrape" {
		t.Errorf("url desc = %q", params["url"].Description)
	}
	if params["question"].Type != "string" {
		t.Errorf("question type = %q, want string (default for empty)", params["question"].Type)
	}
	if params["maxResults"].Type != "number" {
		t.Errorf("maxResults type = %q, want number", params["maxResults"].Type)
	}
	if len(required) != 1 || required[0] != "url" {
		t.Errorf("required = %v, want [url]", required)
	}
}

func TestParseSchemaToParams_NullTypeDefaultsToString(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"data": {"type": "null", "description": "Should become string"}
		}
	}`)
	params, _ := parseSchemaToParams(schema)
	if params["data"].Type != "string" {
		t.Errorf("null type should default to string, got %q", params["data"].Type)
	}
}

func TestParseSchemaToParams_ArrayWithItems(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"tags": {
				"type": "array",
				"description": "List of tags",
				"items": {"type": "string", "description": "A tag"}
			}
		}
	}`)
	params, _ := parseSchemaToParams(schema)
	if params["tags"].Type != "array" {
		t.Fatalf("tags type = %q, want array", params["tags"].Type)
	}
	if params["tags"].Items == nil || params["tags"].Items.Type != "string" {
		t.Fatalf("tags items type = %v", params["tags"].Items)
	}
}

func TestParseSchemaToParams_ObjectWithProperties(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"config": {
				"type": "object",
				"description": "Configuration",
				"properties": {
					"depth": {"type": "number", "description": "Crawl depth"},
					"follow_links": {"type": "boolean", "description": "Follow links"}
				}
			}
		}
	}`)
	params, _ := parseSchemaToParams(schema)
	if params["config"].Type != "object" {
		t.Fatalf("config type = %q, want object", params["config"].Type)
	}
	if params["config"].Items == nil {
		t.Fatal("config items should not be nil")
	}
	if params["config"].Items.Type != "object" {
		t.Errorf("config items type = %q, want object", params["config"].Items.Type)
	}
	if len(params["config"].Items.Properties) != 2 {
		t.Fatalf("config properties count = %d, want 2", len(params["config"].Items.Properties))
	}
	if params["config"].Items.Properties["depth"].Type != "number" {
		t.Errorf("depth type = %q", params["config"].Items.Properties["depth"].Type)
	}
	if params["config"].Items.Properties["follow_links"].Type != "boolean" {
		t.Errorf("follow_links type = %q", params["config"].Items.Properties["follow_links"].Type)
	}
}

func TestParseSchemaToParams_EnumValues(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"format": {"type": "string", "enum": ["markdown", "html", "text"]}
		}
	}`)
	params, _ := parseSchemaToParams(schema)
	if len(params["format"].Enum) != 3 {
		t.Errorf("format enum = %v, want 3 items", params["format"].Enum)
	}
}

func TestParseSchemaToParams_MalformedJSON(t *testing.T) {
	params, required := parseSchemaToParams([]byte(`{invalid json`))
	if params != nil {
		t.Errorf("expected nil for malformed JSON, got %v", params)
	}
	if required != nil {
		t.Errorf("expected nil required for malformed JSON, got %v", required)
	}
}

func TestParseSchemaToParams_NoProperties(t *testing.T) {
	params, required := parseSchemaToParams([]byte(`{"type":"object"}`))
	if params != nil {
		t.Errorf("expected nil for no properties, got %v", params)
	}
	if required != nil {
		t.Errorf("expected nil required, got %v", required)
	}
}

func TestParseSchemaToParams_RealFirecrawlSchema(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL to scrape"},
			"excludeTags": {"type": "array", "description": "Tags to exclude", "items": {"type": "string"}},
			"includeTags": {"type": "array", "description": "Tags to include", "items": {"type": "string"}},
			"onlyMainContent": {"type": "boolean", "description": "Only main content"},
			"waitFor": {"type": "number", "description": "Wait time in ms"}
		},
		"required": ["url"]
	}`)
	params, required := parseSchemaToParams(schema)
	if len(params) != 5 {
		t.Fatalf("expected 5 params, got %d", len(params))
	}
	if params["url"].Type != "string" {
		t.Errorf("url type = %q", params["url"].Type)
	}
	if params["excludeTags"].Type != "array" {
		t.Errorf("excludeTags type = %q", params["excludeTags"].Type)
	}
	if params["onlyMainContent"].Type != "boolean" {
		t.Errorf("onlyMainContent type = %q", params["onlyMainContent"].Type)
	}
	if params["waitFor"].Type != "number" {
		t.Errorf("waitFor type = %q", params["waitFor"].Type)
	}
	if len(required) != 1 || required[0] != "url" {
		t.Errorf("required = %v, want [url]", required)
	}
}

func TestSanitizeUnsanitizeRoundtrip(t *testing.T) {
	tests := []struct {
		qualified   string
		sanitized   string
		unsanitized string
	}{
		{
			qualified:   "remote:ba1f3962-2a78-490e-9d8b-77299c83e9cb.firecrawl_scrape",
			sanitized:   "remote_ba1f3962__firecrawl_scrape",
			unsanitized: "remote:ba1f3962.firecrawl_scrape",
		},
		{
			qualified:   "remote:f5a02483-266a-4400-9906-46b38f86a4fe.ask_question",
			sanitized:   "remote_f5a02483__ask_question",
			unsanitized: "remote:f5a02483.ask_question",
		},
		{
			qualified:   "builtin:notion.search",
			sanitized:   "builtin_notion__search",
			unsanitized: "builtin:notion.search",
		},
	}
	for _, tc := range tests {
		gotSanitized := sanitizeToolName(tc.qualified)
		if gotSanitized != tc.sanitized {
			t.Errorf("sanitize(%q) = %q, want %q", tc.qualified, gotSanitized, tc.sanitized)
		}
		gotUnsanitized := unsanitizeToolName(tc.sanitized)
		if gotUnsanitized != tc.unsanitized {
			t.Errorf("unsanitize(%q) = %q, want %q", tc.sanitized, gotUnsanitized, tc.unsanitized)
		}
	}
}

func TestSanitizeToolNameLength(t *testing.T) {
	longName := "remote:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.some_very_long_tool_name_that_exceeds_sixty_four_characters"
	result := sanitizeToolName(longName)
	if len(result) > 64 {
		t.Errorf("sanitized name length %d exceeds 64: %s", len(result), result)
	}
}

func TestParseSchemaToParams_CreatesValidDefinition(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL"},
			"depth": {"type": "number", "description": "Depth"},
			"follow": {"type": "boolean", "description": "Follow links"}
		},
		"required": ["url"]
	}`)
	params, required := parseSchemaToParams(schema)
	def := tools.Definition{
		Name:               "remote_abc__scrape",
		Description:        "Scrape a URL",
		Parameters:         params,
		Required:           required,
	}

	if def.Name != "remote_abc__scrape" {
		t.Errorf("Name = %q", def.Name)
	}
	if len(def.Parameters) != 3 {
		t.Fatalf("Parameters count = %d, want 3", len(def.Parameters))
	}
	if len(def.Required) != 1 {
		t.Errorf("Required = %v, want 1 item", def.Required)
	}
	for _, p := range []string{"url", "depth", "follow"} {
		param, ok := def.Parameters[p]
		if !ok {
			t.Errorf("missing parameter %q", p)
			continue
		}
		if param.Type == "" {
			t.Errorf("parameter %q has empty type", p)
		}
	}
}