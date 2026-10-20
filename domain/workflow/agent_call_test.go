package workflow

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func agentCallTrigger(inputs ...interface{}) Node {
	return Node{ID: "t", Type: NodeTypeTriggerAgentCall, Config: map[string]interface{}{AgentCallInputsKey: inputs}}
}

func input(name, typ string, required bool) map[string]interface{} {
	return map[string]interface{}{"name": name, "type": typ, "required": required, "description": "d " + name, "example": "e"}
}

func TestTheAgentCallTriggerIsAMessagingOnlyTrigger(t *testing.T) {
	if TriggerAgentCall != "trigger_agent_call" || !TriggerAgentCall.Valid() || TriggerAgentCall.WorkflowType() != WorkflowTypeMessages {
		t.Fatalf("%q must be a valid messaging trigger", TriggerAgentCall)
	}
	if NodeType(TriggerAgentCall) != NodeTypeTriggerAgentCall || !NodeTypeTriggerAgentCall.IsTrigger() || !NodeTypeTriggerAgentCall.Valid() {
		t.Fatal("the agent-call node type must be a valid trigger node")
	}
	def, ok := NodeCatalogMap(BuiltinDefinitions())[NodeTypeTriggerAgentCall]
	if !ok {
		t.Fatal("trigger_agent_call is missing from the catalog")
	}
	if def.Label != "Chamado por agente IA" || def.Category != NodeCategoryTrigger {
		t.Fatalf("definition = %+v", def)
	}
	if !DefinitionAllowedForType(def, WorkflowTypeMessages) || DefinitionAllowedForType(def, WorkflowTypeVoice) {
		t.Fatal("the agent-call trigger belongs to messaging workflows only")
	}
	if len(def.ConfigSchema) != 1 || def.ConfigSchema[0].Key != AgentCallInputsKey {
		t.Fatalf("the trigger must declare its inputs field, got %+v", def.ConfigSchema)
	}
}

func TestTheReplyToAgentNodeIsAMessagingAction(t *testing.T) {
	if !NodeTypeActionReplyToAgent.Valid() || NodeTypeActionReplyToAgent.IsTrigger() {
		t.Fatal("action_reply_to_agent must be a valid non-trigger node")
	}
	if NodeTypeActionReplyToAgent.Category() != NodeCategoryAction {
		t.Fatalf("category = %s", NodeTypeActionReplyToAgent.Category())
	}
}

func TestAgentCallInputsAreReadFromTheTriggerConfig(t *testing.T) {
	g := Graph{Nodes: []Node{agentCallTrigger(input("cpf", "string", true), input("idade", "", false))}}

	got, err := g.AgentCallInputs()
	if err != nil {
		t.Fatal(err)
	}
	want := []AgentCallInput{
		{Name: "cpf", Type: AgentCallInputString, Description: "d cpf", Required: true, Example: "e"},
		{Name: "idade", Type: AgentCallInputString, Description: "d idade", Example: "e"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inputs = %+v, want %+v", got, want)
	}
}

func TestAWorkflowWithoutTheAgentCallTriggerHasNoInputs(t *testing.T) {
	g := Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerWebhook}}}
	if _, err := g.AgentCallInputs(); !errors.Is(err, ErrNoAgentCallTrigger) {
		t.Fatalf("err = %v, want ErrNoAgentCallTrigger", err)
	}
}

func TestAgentCallInputDeclarationsFailClosed(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{"no inputs at all", map[string]interface{}{}, ""},
		{"an empty list", map[string]interface{}{AgentCallInputsKey: []interface{}{}}, ""},
		{"not a list", map[string]interface{}{AgentCallInputsKey: "cpf"}, "lista"},
		{"an item that is not an object", map[string]interface{}{AgentCallInputsKey: []interface{}{"cpf"}}, "objeto"},
		{"an uppercase name", map[string]interface{}{AgentCallInputsKey: []interface{}{input("CPF", "string", true)}}, "CPF"},
		{"a name with a dash", map[string]interface{}{AgentCallInputsKey: []interface{}{input("data-nasc", "string", true)}}, "data-nasc"},
		{"an empty name", map[string]interface{}{AgentCallInputsKey: []interface{}{input("", "string", true)}}, "nome"},
		{"an unknown type", map[string]interface{}{AgentCallInputsKey: []interface{}{input("cpf", "cpf", true)}}, "cpf"},
		{"a duplicated name", map[string]interface{}{AgentCallInputsKey: []interface{}{input("cpf", "string", true), input("cpf", "number", false)}}, "repetido"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AgentCallInputsOf(tc.config)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrAgentCallInputsInvalid) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrAgentCallInputsInvalid mentioning %q", err, tc.want)
			}
		})
	}
}

func TestValidateAgentCallInputsCleansValuesAndNamesMissingFields(t *testing.T) {
	fields := []AgentCallInput{
		{Name: "cpf", Type: AgentCallInputString, Required: true},
		{Name: "idade", Type: AgentCallInputNumber},
		{Name: "aceite", Type: AgentCallInputBoolean, Required: true},
		{Name: "endereco", Type: AgentCallInputObject},
		{Name: "cursos", Type: AgentCallInputArray},
	}
	cases := []struct {
		name        string
		values      map[string]interface{}
		wantValues  map[string]interface{}
		wantMissing []string
	}{
		{
			name:       "unknown keys are dropped",
			values:     map[string]interface{}{"cpf": "123", "aceite": true, "senha": "x"},
			wantValues: map[string]interface{}{"cpf": "123", "aceite": true},
		},
		{
			name:        "absent, null and blank required fields are missing",
			values:      map[string]interface{}{"cpf": "  ", "aceite": nil},
			wantValues:  map[string]interface{}{},
			wantMissing: []string{"cpf", "aceite"},
		},
		{
			name:       "every declared type is accepted",
			values:     map[string]interface{}{"cpf": "1", "idade": float64(30), "aceite": false, "endereco": map[string]interface{}{"cep": "1"}, "cursos": []interface{}{"adm"}},
			wantValues: map[string]interface{}{"cpf": "1", "idade": float64(30), "aceite": false, "endereco": map[string]interface{}{"cep": "1"}, "cursos": []interface{}{"adm"}},
		},
		{
			name:       "integers and json numbers are numbers",
			values:     map[string]interface{}{"cpf": "1", "aceite": true, "idade": json.Number("30")},
			wantValues: map[string]interface{}{"cpf": "1", "aceite": true, "idade": json.Number("30")},
		},
		{
			name:        "no values at all",
			values:      nil,
			wantValues:  map[string]interface{}{},
			wantMissing: []string{"cpf", "aceite"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, missing, err := ValidateAgentCallInputs(fields, tc.values)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.wantValues) {
				t.Fatalf("values = %#v, want %#v", got, tc.wantValues)
			}
			if !reflect.DeepEqual(missing, tc.wantMissing) {
				t.Fatalf("missing = %v, want %v", missing, tc.wantMissing)
			}
		})
	}
}

func TestValidateAgentCallInputsRefusesAWrongType(t *testing.T) {
	cases := []struct {
		name  string
		field AgentCallInput
		value interface{}
	}{
		{"a number for a string", AgentCallInput{Name: "cpf", Type: AgentCallInputString}, float64(123)},
		{"a string for a number", AgentCallInput{Name: "idade", Type: AgentCallInputNumber}, "30"},
		{"a string for a boolean", AgentCallInput{Name: "aceite", Type: AgentCallInputBoolean}, "true"},
		{"a list for an object", AgentCallInput{Name: "endereco", Type: AgentCallInputObject}, []interface{}{}},
		{"an object for a list", AgentCallInput{Name: "cursos", Type: AgentCallInputArray}, map[string]interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ValidateAgentCallInputs([]AgentCallInput{tc.field}, map[string]interface{}{tc.field.Name: tc.value})
			if !errors.Is(err, ErrAgentCallInputType) || !strings.Contains(err.Error(), tc.field.Name) {
				t.Fatalf("err = %v, want ErrAgentCallInputType naming %q", err, tc.field.Name)
			}
		})
	}
}

func TestAgentCallRunDataIsReadByTemplatesAndTheStateLookup(t *testing.T) {
	state := NewRunState()
	for k, v := range AgentCallRunData(map[string]interface{}{"cpf": "12345678900", "endereco": map[string]interface{}{"cep": "01001"}}, "agent-1", "Bia") {
		state.Set(k, v)
	}

	if got := Interpolate("CPF {{input.cpf}} de {{agent.name}} ({{agent.id}})", &state, nil); got != "CPF 12345678900 de Bia (agent-1)" {
		t.Fatalf("template = %q", got)
	}
	if got, ok := state.Lookup("input.endereco.cep"); !ok || got != "01001" {
		t.Fatalf("lookup = %v, %v", got, ok)
	}
}

func TestReplyToAgentFieldsAreReadAsAnOrderedListOrAMap(t *testing.T) {
	list := map[string]interface{}{ReplyToAgentFieldsKey: []interface{}{
		map[string]interface{}{"key": "status", "value": "{{last.status}}"},
		map[string]interface{}{"key": " ", "value": "ignorado"},
		map[string]interface{}{"key": "mensagem", "value": "ok"},
	}}
	if got := ReplyToAgentFieldsOf(list); !reflect.DeepEqual(got, []ReplyToAgentField{{Key: "status", Value: "{{last.status}}"}, {Key: "mensagem", Value: "ok"}}) {
		t.Fatalf("list fields = %+v", got)
	}

	asMap := map[string]interface{}{ReplyToAgentFieldsKey: map[string]interface{}{"b": "2", "a": "1"}}
	if got := ReplyToAgentFieldsOf(asMap); !reflect.DeepEqual(got, []ReplyToAgentField{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}) {
		t.Fatalf("map fields = %+v", got)
	}
}

func TestReplyToAgentValuesInterpolateEachField(t *testing.T) {
	state := NewRunState()
	state.Set("_last_status", "aprovado")
	got := ReplyToAgentValues([]ReplyToAgentField{{Key: "status", Value: "{{last.status}}"}, {Key: "segredo", Value: "{{secret.token}}"}}, &state)
	want := map[string]interface{}{"status": "aprovado", "segredo": "{{secret.token}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %#v, want %#v", got, want)
	}
}

func lintCodes(g *Graph) map[LintIssueCode]LintIssue {
	report := LintGraph(g, WorkflowTypeMessages, BuiltinDefinitions(), nil, nil)
	out := map[LintIssueCode]LintIssue{}
	for _, issue := range report.Issues {
		out[issue.Code] = issue
	}
	return out
}

func TestLintAcceptsAnAgentCallTriggerWithoutInputs(t *testing.T) {
	g := &Graph{
		Nodes: []Node{agentCallTrigger(), {ID: "r", Type: NodeTypeActionReplyToAgent}, {ID: "end", Type: NodeTypeEnd}},
		Edges: []Edge{{Source: "t", Target: "r"}, {Source: "r", Target: "end"}},
	}
	issues := lintCodes(g)
	if _, found := issues[LintAgentCallInputsInvalid]; found {
		t.Fatalf("no inputs must be fine: %+v", issues)
	}
	if _, found := issues[LintReplyToAgentWithoutTrigger]; found {
		t.Fatalf("the reply node is allowed with the trigger: %+v", issues)
	}
}

func TestLintBlocksADuplicatedAgentCallInput(t *testing.T) {
	g := &Graph{
		Nodes: []Node{agentCallTrigger(input("cpf", "string", true), input("cpf", "string", false)), {ID: "end", Type: NodeTypeEnd}},
		Edges: []Edge{{Source: "t", Target: "end"}},
	}
	issue, found := lintCodes(g)[LintAgentCallInputsInvalid]
	if !found || issue.Severity != SeverityBlocking || issue.NodeID != "t" || issue.Field != AgentCallInputsKey {
		t.Fatalf("issue = %+v, found = %v", issue, found)
	}
}

func TestLintBlocksAReplyToAgentNodeWithoutTheAgentCallTrigger(t *testing.T) {
	g := &Graph{
		Nodes: []Node{{ID: "t", Type: NodeTypeTriggerWebhook}, {ID: "r", Type: NodeTypeActionReplyToAgent}, {ID: "end", Type: NodeTypeEnd}},
		Edges: []Edge{{Source: "t", Target: "r"}, {Source: "r", Target: "end"}},
	}
	issue, found := lintCodes(g)[LintReplyToAgentWithoutTrigger]
	if !found || issue.Severity != SeverityBlocking || issue.NodeID != "r" {
		t.Fatalf("issue = %+v, found = %v", issue, found)
	}
}
