package workflow

import (
	"strings"
	"testing"
)

func TestInterpolate_VarScope(t *testing.T) {
	state := NewRunState()
	state.Set("name", "John")
	state.Set("age", 30)

	got := Interpolate("Hello {{var.name}}, you are {{var.age}} years old", &state, nil)
	want := "Hello John, you are 30 years old"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_ArrayIndexing(t *testing.T) {
	// Mirrors an HTTP node response captured into a variable: data is an array.
	resp := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"link_2via": "https://example.com/boleto/1"},
			map[string]interface{}{"link_2via": "https://example.com/boleto/2"},
		},
	}
	state := NewRunState()
	state.Set("resposta_pendencia", resp)
	state.Set("_last_data", resp["data"])

	cases := map[string]string{
		"{{resposta_pendencia.data[0].link_2via}}":   "https://example.com/boleto/1",
		"{{resposta_pendencia.data[1].link_2via}}":   "https://example.com/boleto/2",
		"{{ resposta_pendencia.data[0].link_2via }}": "https://example.com/boleto/1",             // whitespace
		"{{last.data[1].link_2via}}":                 "https://example.com/boleto/2",             // last scope
		"{{resposta_pendencia.data[9].link_2via}}":   "{{resposta_pendencia.data[9].link_2via}}", // out of range -> literal
	}
	for tmpl, want := range cases {
		if got := Interpolate(tmpl, &state, nil); got != want {
			t.Errorf("Interpolate(%q) = %q, want %q", tmpl, got, want)
		}
	}
}

func TestInterpolate_MissingVar(t *testing.T) {
	state := NewRunState()
	got := Interpolate("Hello {{var.missing}}", &state, nil)
	want := "Hello {{var.missing}}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_SysScope(t *testing.T) {
	state := NewRunState()
	sysCtx := map[string]interface{}{
		"entry_name":  "Alice",
		"entry_phone": "+123",
	}
	got := Interpolate("Name: {{sys.entry_name}}, Phone: {{sys.entry_phone}}", &state, sysCtx)
	want := "Name: Alice, Phone: +123"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_SysBuiltinDate(t *testing.T) {
	state := NewRunState()
	got := Interpolate("Today is {{sys.date}}", &state, nil)

	if !strings.Contains(got, "Today is 20") {
		t.Fatalf("expected date interpolation, got %q", got)
	}
	if strings.Contains(got, "{{") {
		t.Fatalf("expected no unresolved placeholders, got %q", got)
	}
}

func TestInterpolate_SysBuiltinTimestamp(t *testing.T) {
	state := NewRunState()
	got := Interpolate("ts={{sys.timestamp}}", &state, nil)
	if strings.Contains(got, "{{") {
		t.Fatalf("expected no unresolved placeholders, got %q", got)
	}
}

func TestInterpolate_SysUnknown(t *testing.T) {
	state := NewRunState()
	got := Interpolate("{{sys.unknown_key}}", &state, nil)
	want := "{{sys.unknown_key}}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_LastScope(t *testing.T) {
	state := NewRunState()
	state.Set("_last_reply", "yes please")
	got := Interpolate("User said: {{last.reply}}", &state, nil)
	want := "User said: yes please"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_AIScope(t *testing.T) {
	state := NewRunState()
	state.Set("_ai_sentiment", "positive")
	got := Interpolate("Sentiment: {{ai.sentiment}}", &state, nil)
	want := "Sentiment: positive"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_UnknownScope(t *testing.T) {
	state := NewRunState()
	got := Interpolate("{{foo.bar}}", &state, nil)
	want := "{{foo.bar}}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_NoPlaceholders(t *testing.T) {
	state := NewRunState()
	text := "plain text without any placeholders"
	got := Interpolate(text, &state, nil)
	if got != text {
		t.Fatalf("got %q, want %q", got, text)
	}
}

func TestInterpolate_MultipleSameVar(t *testing.T) {
	state := NewRunState()
	state.Set("x", "A")
	got := Interpolate("{{var.x}} and {{var.x}}", &state, nil)
	want := "A and A"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_MixedScopes(t *testing.T) {
	state := NewRunState()
	state.Set("name", "Bob")
	state.Set("_last_reply", "hello")
	state.Set("_ai_intent", "greeting")
	sysCtx := map[string]interface{}{"workspace": "ws1"}

	got := Interpolate("{{var.name}} said {{last.reply}}, intent={{ai.intent}}, ws={{sys.workspace}}", &state, sysCtx)
	want := "Bob said hello, intent=greeting, ws=ws1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_MalformedBraces(t *testing.T) {
	state := NewRunState()

	got := Interpolate("{var.name} and {{bad_no_dot}}", &state, nil)
	want := "{var.name} and {{bad_no_dot}}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_BareVariable(t *testing.T) {
	state := NewRunState()
	state.Set("message", "hello world")
	got := Interpolate("User said: {{message}}", &state, nil)
	want := "User said: hello world"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolateMap_StringValues(t *testing.T) {
	state := NewRunState()
	state.Set("url", "https://example.com")

	m := map[string]interface{}{
		"url":    "{{var.url}}",
		"method": "GET",
		"count":  42,
	}
	result := InterpolateMap(m, &state, nil)
	if result["url"] != "https://example.com" {
		t.Fatalf("expected url to be interpolated, got %v", result["url"])
	}
	if result["method"] != "GET" {
		t.Fatalf("expected method unchanged, got %v", result["method"])
	}
	if result["count"] != 42 {
		t.Fatalf("expected count unchanged, got %v", result["count"])
	}
}

func TestInterpolateMap_NestedMap(t *testing.T) {
	state := NewRunState()
	state.Set("token", "abc123")

	m := map[string]interface{}{
		"headers": map[string]interface{}{
			"Authorization": "Bearer {{var.token}}",
		},
	}
	result := InterpolateMap(m, &state, nil)
	headers := result["headers"].(map[string]interface{})
	if headers["Authorization"] != "Bearer abc123" {
		t.Fatalf("expected nested interpolation, got %v", headers["Authorization"])
	}
}

func TestInterpolateMap_Nil(t *testing.T) {
	state := NewRunState()
	result := InterpolateMap(nil, &state, nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestInterpolate_NodeScope(t *testing.T) {
	state := NewRunState()
	state.Set("_node_abc123_body", "response body here")
	state.Set("_node_abc123_status_code", 200)

	got := Interpolate("body={{node_abc123.body}} code={{node_abc123.status_code}}", &state, nil)
	want := "body=response body here code=200"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_CaptureVariableMap(t *testing.T) {
	state := NewRunState()
	state.Set("respostacep", map[string]interface{}{
		"bairro":     "Sé",
		"logradouro": "Praça da Sé",
		"cep":        "01001-000",
	})

	got := Interpolate("Bairro: {{respostacep.bairro}}, CEP: {{respostacep.cep}}", &state, nil)
	want := "Bairro: Sé, CEP: 01001-000"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_CaptureVariableBare(t *testing.T) {
	state := NewRunState()
	state.Set("respostacep", map[string]interface{}{
		"bairro": "Sé",
		"cep":    "01001-000",
	})

	got := Interpolate("Resposta: {{respostacep}}", &state, nil)

	if got == "Resposta: {{respostacep}}" {
		t.Fatal("expected interpolation, got literal placeholder")
	}
	if !strings.Contains(got, "bairro") {
		t.Fatalf("expected JSON-encoded map containing 'bairro', got %q", got)
	}
}

func TestInterpolate_CaptureVariableString(t *testing.T) {
	state := NewRunState()
	state.Set("rawbody", "plain text response")

	got := Interpolate("Body: {{rawbody}}", &state, nil)
	want := "Body: plain text response"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInterpolate_CaptureVariableDeepNested(t *testing.T) {
	state := NewRunState()
	state.Set("ia_classificadora", map[string]interface{}{
		"response_text": "ok",
		"tool_args": map[string]interface{}{
			"texto": "algum texto",
		},
	})

	got := Interpolate("Resultado: {{ia_classificadora.tool_args.texto}}", &state, nil)
	want := "Resultado: algum texto"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractDependencies_NoDependencies(t *testing.T) {
	config := map[string]interface{}{
		"url":    "https://api.example.com/data",
		"method": "GET",
		"count":  42,
	}

	deps := ExtractDependencies(config)
	if len(deps) != 0 {
		t.Fatalf("expected no dependencies, got %d: %+v", len(deps), deps)
	}
}

func TestExtractDependencies_LastScope(t *testing.T) {
	config := map[string]interface{}{
		"text": "Status: {{last.status_code}}, Body: {{last.body}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}

	found := false
	for _, d := range deps {
		if d.Scope == "last" && d.Key == "status_code" && d.FullRef == "last.status_code" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find last.status_code dependency")
	}

	found = false
	for _, d := range deps {
		if d.Scope == "last" && d.Key == "body" && d.FullRef == "last.body" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find last.body dependency")
	}
}

func TestExtractDependencies_NodeScope(t *testing.T) {
	config := map[string]interface{}{
		"url": "https://api.com/{{node.http1.response_body}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d: %+v", len(deps), deps)
	}

	d := deps[0]
	if d.Scope != "node" {
		t.Fatalf("expected scope 'node', got %q", d.Scope)
	}
	if d.NodeID != "http1" {
		t.Fatalf("expected nodeID 'http1', got %q", d.NodeID)
	}
	if d.Key != "response_body" {
		t.Fatalf("expected key 'response_body', got %q", d.Key)
	}
	if d.FullRef != "node.http1.response_body" {
		t.Fatalf("expected fullRef 'node.http1.response_body', got %q", d.FullRef)
	}
}

func TestExtractDependencies_VarScope(t *testing.T) {
	config := map[string]interface{}{
		"text": "Hello {{var.name}}, message: {{message}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}

	found := false
	for _, d := range deps {
		if d.Scope == "var" && d.Key == "name" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find var.name dependency")
	}

	found = false
	for _, d := range deps {
		if d.Scope == "var" && d.Key == "message" && d.FullRef == "message" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find bare variable 'message' dependency")
	}
}

func TestExtractDependencies_AIScope(t *testing.T) {
	config := map[string]interface{}{
		"text": "AI response: {{ai.response}}, tool: {{ai.tool_name}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}

	for _, d := range deps {
		if d.Scope != "ai" {
			t.Fatalf("expected scope 'ai', got %q", d.Scope)
		}
	}
}

func TestExtractDependencies_SysScope(t *testing.T) {
	config := map[string]interface{}{
		"timestamp": "{{sys.timestamp}}",
		"date":      "{{sys.date}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}

	for _, d := range deps {
		if d.Scope != "sys" {
			t.Fatalf("expected scope 'sys', got %q", d.Scope)
		}
	}
}

func TestExtractDependencies_CustomCaptureVariable(t *testing.T) {
	config := map[string]interface{}{
		"cep": "{{respostacep.cep}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d: %+v", len(deps), deps)
	}

	d := deps[0]
	if d.Scope != "respostacep" {
		t.Fatalf("expected scope 'respostacep', got %q", d.Scope)
	}
	if d.Key != "cep" {
		t.Fatalf("expected key 'cep', got %q", d.Key)
	}
}

func TestExtractDependencies_NestedMap(t *testing.T) {
	config := map[string]interface{}{
		"headers": map[string]interface{}{
			"Authorization": "Bearer {{var.token}}",
			"X-Request-ID":  "{{last.request_id}}",
		},
		"body": map[string]interface{}{
			"data": "{{node.fetch.result}}",
		},
	}

	deps := ExtractDependencies(config)
	if len(deps) != 3 {
		t.Fatalf("expected 3 dependencies, got %d: %+v", len(deps), deps)
	}
}

func TestExtractDependencies_Array(t *testing.T) {
	config := map[string]interface{}{
		"items": []interface{}{
			"{{last.item1}}",
			"{{last.item2}}",
		},
	}

	deps := ExtractDependencies(config)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}
}

func TestExtractDependencies_DeduplicatesSameRef(t *testing.T) {
	config := map[string]interface{}{
		"text1": "{{last.body}}",
		"text2": "{{last.body}}",
		"text3": "Hello {{last.body}} world",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 1 {
		t.Fatalf("expected 1 deduplicated dependency, got %d: %+v", len(deps), deps)
	}
}

func TestExtractDependencies_MixedScopes(t *testing.T) {
	config := map[string]interface{}{
		"text": "User {{var.name}} sent {{message}}, AI: {{ai.response}}, HTTP: {{last.body}}, Node: {{node.http1.status}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 5 {
		t.Fatalf("expected 5 dependencies, got %d: %+v", len(deps), deps)
	}

	scopes := make(map[string]bool)
	for _, d := range deps {
		scopes[d.Scope] = true
	}

	expectedScopes := []string{"var", "ai", "last", "node"}
	for _, s := range expectedScopes {
		if !scopes[s] {
			t.Fatalf("expected scope %q to be present", s)
		}
	}
}

func TestExtractDependencies_DeepNestedPath(t *testing.T) {
	config := map[string]interface{}{
		"value": "{{last.tool_args.address.city}}",
	}

	deps := ExtractDependencies(config)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d: %+v", len(deps), deps)
	}

	d := deps[0]
	if d.Scope != "last" {
		t.Fatalf("expected scope 'last', got %q", d.Scope)
	}
	if d.Key != "tool_args.address.city" {
		t.Fatalf("expected key 'tool_args.address.city', got %q", d.Key)
	}
}

func TestRequiresUpstreamData_NoDeps(t *testing.T) {
	deps := []VariableDependency{}
	if RequiresUpstreamData(deps) {
		t.Fatal("expected false for empty deps")
	}
}

func TestRequiresUpstreamData_OnlyVarAndSys(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "var", Key: "name"},
		{Scope: "sys", Key: "date"},
		{Scope: "var", Key: "message"},
	}
	if RequiresUpstreamData(deps) {
		t.Fatal("expected false for only var/sys deps")
	}
}

func TestRequiresUpstreamData_HasLast(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "var", Key: "name"},
		{Scope: "last", Key: "body"},
	}
	if !RequiresUpstreamData(deps) {
		t.Fatal("expected true when last scope present")
	}
}

func TestRequiresUpstreamData_HasNode(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "node", NodeID: "http1", Key: "status"},
	}
	if !RequiresUpstreamData(deps) {
		t.Fatal("expected true when node scope present")
	}
}

func TestRequiresUpstreamData_HasAI(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "ai", Key: "response"},
	}
	if !RequiresUpstreamData(deps) {
		t.Fatal("expected true when ai scope present")
	}
}

func TestRequiresUpstreamData_CustomCaptureVariable(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "respostacep", Key: "bairro"},
	}
	if !RequiresUpstreamData(deps) {
		t.Fatal("expected true for custom capture variable")
	}
}

func TestHasAIDependencies_NoDeps(t *testing.T) {
	deps := []VariableDependency{}
	if HasAIDependencies(deps) {
		t.Fatal("expected false for empty deps")
	}
}

func TestHasAIDependencies_OnlyLast(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "last", Key: "body"},
	}
	if HasAIDependencies(deps) {
		t.Fatal("expected false for only last deps")
	}
}

func TestHasAIDependencies_HasAI(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "ai", Key: "response"},
	}
	if !HasAIDependencies(deps) {
		t.Fatal("expected true when ai scope present")
	}
}

func TestHasAIDependencies_CustomCaptureVariable(t *testing.T) {

	deps := []VariableDependency{
		{Scope: "ia_classificadora", Key: "tool_args"},
	}
	if HasAIDependencies(deps) {
		t.Fatal("expected false — custom scopes require graph-level analysis")
	}
}

func TestGetRequiredMocks_Empty(t *testing.T) {
	deps := []VariableDependency{}
	mocks := GetRequiredMocks(deps)
	if len(mocks) != 0 {
		t.Fatalf("expected no mocks, got %d", len(mocks))
	}
}

func TestGetRequiredMocks_LastScope(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "last", Key: "body", FullRef: "last.body"},
		{Scope: "last", Key: "status_code", FullRef: "last.status_code"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 2 {
		t.Fatalf("expected 2 mocks, got %d: %+v", len(mocks), mocks)
	}

	found := false
	for _, m := range mocks {
		if m.StateKey == "_last_body" && m.DisplayName == "last.body" && m.Source == DependencySourcePreviousNode {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected mock for _last_body")
	}
}

func TestGetRequiredMocks_NodeScope(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "node", NodeID: "http1", Key: "status", FullRef: "node.http1.status"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 1 {
		t.Fatalf("expected 1 mock, got %d: %+v", len(mocks), mocks)
	}

	m := mocks[0]
	if m.StateKey != "_node_http1_status" {
		t.Fatalf("expected stateKey '_node_http1_status', got %q", m.StateKey)
	}
	if m.DisplayName != "node.http1.status" {
		t.Fatalf("expected displayName 'node.http1.status', got %q", m.DisplayName)
	}
	if m.Source != DependencySourceSpecificNode {
		t.Fatalf("expected source specific_node, got %q", m.Source)
	}
	if m.SourceNode != "http1" {
		t.Fatalf("expected sourceNode 'http1', got %q", m.SourceNode)
	}
}

func TestGetRequiredMocks_AIScope(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "ai", Key: "response", FullRef: "ai.response"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 1 {
		t.Fatalf("expected 1 mock, got %d: %+v", len(mocks), mocks)
	}

	m := mocks[0]
	if m.StateKey != "_ai_response" {
		t.Fatalf("expected stateKey '_ai_response', got %q", m.StateKey)
	}
	if m.Source != DependencySourceAI {
		t.Fatalf("expected source ai, got %q", m.Source)
	}
}

func TestGetRequiredMocks_VarScope(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "var", Key: "message", FullRef: "message"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 1 {
		t.Fatalf("expected 1 mock, got %d: %+v", len(mocks), mocks)
	}

	m := mocks[0]
	if m.StateKey != "message" {
		t.Fatalf("expected stateKey 'message', got %q", m.StateKey)
	}
	if m.Source != DependencySourceTrigger {
		t.Fatalf("expected source trigger, got %q", m.Source)
	}
}

func TestGetRequiredMocks_SysScope_NoMock(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "sys", Key: "date", FullRef: "sys.date"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 0 {
		t.Fatalf("expected no mocks for sys scope, got %d: %+v", len(mocks), mocks)
	}
}

func TestGetRequiredMocks_CustomScope(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "respostacep", Key: "bairro", FullRef: "respostacep.bairro"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 1 {
		t.Fatalf("expected 1 mock, got %d: %+v", len(mocks), mocks)
	}

	m := mocks[0]
	if m.StateKey != "respostacep.bairro" {
		t.Fatalf("expected stateKey 'respostacep.bairro', got %q", m.StateKey)
	}
	if m.Source != DependencySourceCustom {
		t.Fatalf("expected source custom, got %q", m.Source)
	}
}

func TestGetRequiredMocks_AIScopeNestedPath(t *testing.T) {
	deps := []VariableDependency{{Scope: "ai", Key: "tool_args.cep", FullRef: "ai.tool_args.cep"}}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 1 {
		t.Fatalf("expected 1 mock, got %d: %+v", len(mocks), mocks)
	}
	if mocks[0].StateKey != "_ai_tool_args.cep" {
		t.Fatalf("expected nested ai mock stateKey, got %q", mocks[0].StateKey)
	}
}

func TestGetRequiredMocks_Deduplicates(t *testing.T) {
	deps := []VariableDependency{
		{Scope: "last", Key: "body", FullRef: "last.body"},
		{Scope: "last", Key: "body", FullRef: "last.body"},
	}

	mocks := GetRequiredMocks(deps)
	if len(mocks) != 1 {
		t.Fatalf("expected 1 deduplicated mock, got %d: %+v", len(mocks), mocks)
	}
}

func TestCanTestIndependently_NoVariables(t *testing.T) {
	config := map[string]interface{}{
		"url":    "https://api.example.com",
		"method": "GET",
	}

	if !CanTestIndependently(config) {
		t.Fatal("expected true for config with no variables")
	}
}

func TestCanTestIndependently_OnlySysVariables(t *testing.T) {
	config := map[string]interface{}{
		"timestamp": "{{sys.timestamp}}",
	}

	if !CanTestIndependently(config) {
		t.Fatal("expected true for config with only sys variables")
	}
}

func TestCanTestIndependently_OnlyVarVariables(t *testing.T) {
	config := map[string]interface{}{
		"text": "Hello {{message}}",
	}

	if !CanTestIndependently(config) {
		t.Fatal("expected true for config with only var variables")
	}
}

func TestCanTestIndependently_HasLastVariable(t *testing.T) {
	config := map[string]interface{}{
		"text": "Response: {{last.body}}",
	}

	if CanTestIndependently(config) {
		t.Fatal("expected false for config with last variable")
	}
}

func TestCanTestIndependently_HasNodeVariable(t *testing.T) {
	config := map[string]interface{}{
		"url": "https://api.com/{{node.http1.id}}",
	}

	if CanTestIndependently(config) {
		t.Fatal("expected false for config with node variable")
	}
}

func TestCanTestIndependently_HasAIVariable(t *testing.T) {
	config := map[string]interface{}{
		"text": "AI said: {{ai.response}}",
	}

	if CanTestIndependently(config) {
		t.Fatal("expected false for config with ai variable")
	}
}

func TestCanTestIndependently_HasCustomCaptureVariable(t *testing.T) {
	config := map[string]interface{}{
		"cep": "{{respostacep.cep}}",
	}

	if CanTestIndependently(config) {
		t.Fatal("expected false for config with custom capture variable")
	}
}

func TestExtractCampaignVars_EmptyGraph(t *testing.T) {
	g := Graph{}
	got := ExtractCampaignVars(g)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestExtractCampaignVars_NoCampvars(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{"text": "Hello {{var.name}}"}},
			{ID: "2", Config: map[string]interface{}{"url": "https://example.com"}},
		},
	}
	got := ExtractCampaignVars(g)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestExtractCampaignVars_VarCampvarsPrefix(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{"text": "City: {{var.campvars.city}}"}},
		},
	}
	got := ExtractCampaignVars(g)
	if len(got) != 1 || got[0] != "city" {
		t.Fatalf("expected [city], got %v", got)
	}
}

func TestExtractCampaignVars_ShortPrefix(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{"text": "City: {{campvars.city}}"}},
		},
	}
	got := ExtractCampaignVars(g)
	if len(got) != 1 || got[0] != "city" {
		t.Fatalf("expected [city], got %v", got)
	}
}

func TestExtractCampaignVars_MultipleNodes(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{"text": "{{var.campvars.city}}"}},
			{ID: "2", Config: map[string]interface{}{"msg": "Hello {{campvars.name}}"}},
			{ID: "3", Config: map[string]interface{}{"text": "{{var.campvars.age}}"}},
		},
	}
	got := ExtractCampaignVars(g)
	if len(got) != 3 {
		t.Fatalf("expected 3 vars, got %v", got)
	}

	if got[0] != "age" || got[1] != "city" || got[2] != "name" {
		t.Fatalf("expected [age city name], got %v", got)
	}
}

func TestExtractCampaignVars_Deduplication(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{"a": "{{var.campvars.city}}"}},
			{ID: "2", Config: map[string]interface{}{"b": "{{campvars.city}}"}},
		},
	}
	got := ExtractCampaignVars(g)
	if len(got) != 1 || got[0] != "city" {
		t.Fatalf("expected [city], got %v", got)
	}
}

func TestExtractCampaignVars_NestedConfig(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{
				"headers": map[string]interface{}{
					"X-City": "{{var.campvars.city}}",
				},
				"items": []interface{}{
					"{{campvars.item1}}",
					"{{var.campvars.item2}}",
				},
			}},
		},
	}
	got := ExtractCampaignVars(g)
	if len(got) != 3 {
		t.Fatalf("expected 3 vars, got %v", got)
	}
	if got[0] != "city" || got[1] != "item1" || got[2] != "item2" {
		t.Fatalf("expected [city item1 item2], got %v", got)
	}
}

func TestExtractCampaignVars_MultipleInSameString(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "1", Config: map[string]interface{}{
				"text": "Hello {{campvars.name}}, you live in {{var.campvars.city}}",
			}},
		},
	}
	got := ExtractCampaignVars(g)
	if len(got) != 2 {
		t.Fatalf("expected 2 vars, got %v", got)
	}
	if got[0] != "city" || got[1] != "name" {
		t.Fatalf("expected [city name], got %v", got)
	}
}

func TestCanTestIndependently_ComplexConfig(t *testing.T) {

	config := map[string]interface{}{
		"url": "https://api.com/{{sys.timestamp}}",
		"headers": map[string]interface{}{
			"X-User": "{{var.user}}",
		},
		"body": map[string]interface{}{
			"message": "{{message}}",
		},
	}

	if !CanTestIndependently(config) {
		t.Fatal("expected true for config with only var/sys variables")
	}
}

func TestParseVariableRef_BareVariable(t *testing.T) {
	dep := parseVariableRef("message")

	if dep.Scope != "var" {
		t.Fatalf("expected scope 'var', got %q", dep.Scope)
	}
	if dep.Key != "message" {
		t.Fatalf("expected key 'message', got %q", dep.Key)
	}
	if dep.FullRef != "message" {
		t.Fatalf("expected fullRef 'message', got %q", dep.FullRef)
	}
}

func TestParseVariableRef_VarScope(t *testing.T) {
	dep := parseVariableRef("var.name")

	if dep.Scope != "var" {
		t.Fatalf("expected scope 'var', got %q", dep.Scope)
	}
	if dep.Key != "name" {
		t.Fatalf("expected key 'name', got %q", dep.Key)
	}
}

func TestParseVariableRef_LastScope(t *testing.T) {
	dep := parseVariableRef("last.body")

	if dep.Scope != "last" {
		t.Fatalf("expected scope 'last', got %q", dep.Scope)
	}
	if dep.Key != "body" {
		t.Fatalf("expected key 'body', got %q", dep.Key)
	}
}

func TestParseVariableRef_NodeScope(t *testing.T) {
	dep := parseVariableRef("node.http1.status")

	if dep.Scope != "node" {
		t.Fatalf("expected scope 'node', got %q", dep.Scope)
	}
	if dep.NodeID != "http1" {
		t.Fatalf("expected nodeID 'http1', got %q", dep.NodeID)
	}
	if dep.Key != "status" {
		t.Fatalf("expected key 'status', got %q", dep.Key)
	}
}

func TestParseVariableRef_NodeScopeNoKey(t *testing.T) {
	dep := parseVariableRef("node.http1")

	if dep.Scope != "node" {
		t.Fatalf("expected scope 'node', got %q", dep.Scope)
	}
	if dep.NodeID != "http1" {
		t.Fatalf("expected nodeID 'http1', got %q", dep.NodeID)
	}
	if dep.Key != "" {
		t.Fatalf("expected empty key, got %q", dep.Key)
	}
}

func TestParseVariableRef_DeepPath(t *testing.T) {
	dep := parseVariableRef("last.tool_args.cep")

	if dep.Scope != "last" {
		t.Fatalf("expected scope 'last', got %q", dep.Scope)
	}
	if dep.Key != "tool_args.cep" {
		t.Fatalf("expected key 'tool_args.cep', got %q", dep.Key)
	}
}
