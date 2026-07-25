package node_executors

import (
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestBuildStateContext_Empty(t *testing.T) {
	state := workflow.NewRunState()
	result := buildStateContext(&state)
	if result != "" {
		t.Fatalf("expected empty string for empty state, got %q", result)
	}
}

func TestBuildStateContext_NilState(t *testing.T) {
	result := buildStateContext(nil)
	if result != "" {
		t.Fatalf("expected empty string for nil state, got %q", result)
	}
}

func TestBuildStateContext_LastVars(t *testing.T) {
	state := workflow.NewRunState()
	state.Set("_last_status_code", 404)
	state.Set("_last_body", `{"error":"not found"}`)
	state.Set("_last_success", false)

	result := buildStateContext(&state)
	if !strings.Contains(result, "Previous step output:") {
		t.Fatal("expected 'Previous step output:' header")
	}
	if !strings.Contains(result, "status_code: 404") {
		t.Fatalf("expected status_code in output, got:\n%s", result)
	}
	if !strings.Contains(result, "body:") {
		t.Fatalf("expected body in output, got:\n%s", result)
	}
}

func TestBuildStateContext_UserVars(t *testing.T) {
	state := workflow.NewRunState()
	state.Set("name", "Alice")
	state.Set("color", "blue")

	result := buildStateContext(&state)
	if !strings.Contains(result, "Variables:") {
		t.Fatal("expected 'Variables:' header")
	}
	if !strings.Contains(result, "name: Alice") {
		t.Fatalf("expected name in output, got:\n%s", result)
	}
	if !strings.Contains(result, "color: blue") {
		t.Fatalf("expected color in output, got:\n%s", result)
	}
}

func TestBuildStateContext_SkipsInternalVars(t *testing.T) {
	state := workflow.NewRunState()
	state.Set("_wait_outcome", "replied")
	state.Set("_node_n1_status_code", 200)
	state.Set("_ai_response", "hello")
	state.Set("_last_body", "ok")
	state.Set("name", "Bob")

	result := buildStateContext(&state)
	if strings.Contains(result, "_wait_outcome") {
		t.Fatal("should not contain _wait_outcome")
	}
	if strings.Contains(result, "_node_") {
		t.Fatal("should not contain _node_ vars")
	}
	if strings.Contains(result, "_ai_") {
		t.Fatal("should not contain _ai_ vars")
	}
	if !strings.Contains(result, "body: ok") {
		t.Fatal("expected _last_body to appear as 'body'")
	}
	if !strings.Contains(result, "name: Bob") {
		t.Fatal("expected user var 'name'")
	}
}

func TestBuildStateContext_TruncatesLongValues(t *testing.T) {
	state := workflow.NewRunState()
	longVal := strings.Repeat("x", 600)
	state.Set("_last_body", longVal)

	result := buildStateContext(&state)
	if !strings.Contains(result, "...") {
		t.Fatal("expected truncated value with ...")
	}

	if strings.Contains(result, strings.Repeat("x", 501)) {
		t.Fatal("value should be truncated at 500 chars")
	}
}

func TestBuildStateContext_JSONMapValue(t *testing.T) {
	state := workflow.NewRunState()
	state.Set("_last_json", map[string]interface{}{"foo": "bar", "count": 42})

	result := buildStateContext(&state)
	if !strings.Contains(result, "foo") {
		t.Fatalf("expected JSON key in output, got:\n%s", result)
	}
}

func TestFormatStateValue_Types(t *testing.T) {
	tests := []struct {
		input    interface{}
		contains string
	}{
		{"hello", "hello"},
		{42, "42"},
		{true, "true"},
		{3.14, "3.14"},
		{map[string]interface{}{"a": 1}, `"a":1`},
		{[]interface{}{"x", "y"}, `["x","y"]`},
	}
	for _, tt := range tests {
		result := formatStateValue(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("formatStateValue(%v) = %q, expected to contain %q", tt.input, result, tt.contains)
		}
	}
}
