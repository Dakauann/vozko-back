package workflow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/tracepayload"
)

func triggerGraph(trigger Node, readers ...Node) *Graph {
	g := &Graph{Nodes: []Node{trigger}}
	prev := trigger.ID
	for _, n := range readers {
		g.Nodes = append(g.Nodes, n)
		g.Edges = append(g.Edges, Edge{Source: prev, Target: n.ID})
		prev = n.ID
	}
	g.Nodes = append(g.Nodes, Node{ID: "end", Type: NodeTypeEnd})
	g.Edges = append(g.Edges, Edge{Source: prev, Target: "end"})
	return g
}

func sendText(id, text string) Node {
	return Node{ID: id, Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": text}}
}

func agentCallTestTrigger() Node {
	return Node{ID: "t", Type: NodeTypeTriggerAgentCall, Config: map[string]interface{}{
		"display_name": "Chamado pela Bia",
		AgentCallInputsKey: []interface{}{
			map[string]interface{}{"name": "cpf", "type": "string", "required": true, "description": "CPF do candidato", "example": "52998224725"},
			map[string]interface{}{"name": "idade", "type": "number"},
			map[string]interface{}{"name": "aceite", "type": "boolean"},
			map[string]interface{}{"name": "endereco", "type": "object"},
			map[string]interface{}{"name": "cursos", "type": "array"},
		},
	}}
}

func onlySpec(t *testing.T, g *Graph, sources TriggerInputSources) TriggerInputSpec {
	t.Helper()
	specs := TriggerInputSpecsOf(g, sources)
	if len(specs) != 1 {
		t.Fatalf("specs = %+v, want one per trigger node", specs)
	}
	return specs[0]
}

func fieldByKey(spec TriggerInputSpec, key string) *TriggerInputField {
	for i := range spec.Fields {
		if spec.Fields[i].Key == key {
			return &spec.Fields[i]
		}
	}
	return nil
}

func TestTheAgentCallSpecListsTheDeclaredInputsTheAgentNameAndWhoReadsThem(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(), sendText("n1", "CPF {{input.cpf}} de {{agent.name}}"), sendText("n2", "{{var.input.cpf}} {{input.matricula}} {{input.endereco.cep}}"))
	spec := onlySpec(t, g, TriggerInputSources{})

	if spec.NodeID != "t" || spec.TriggerType != TriggerAgentCall || spec.Shape != TriggerInputFields || spec.Label != "Chamado pela Bia" || !spec.Readable() {
		t.Fatalf("spec = %+v", spec)
	}
	cpf := fieldByKey(spec, "cpf")
	if cpf == nil || cpf.Type != TriggerFieldText || !cpf.Required || cpf.Description != "CPF do candidato" || cpf.Example != "52998224725" || !reflect.DeepEqual(cpf.ReadBy, []string{"n1", "n2"}) {
		t.Fatalf("cpf = %+v", cpf)
	}
	wantTypes := map[string]TriggerInputFieldType{"idade": TriggerFieldNumber, "aceite": TriggerFieldBoolean, "endereco": TriggerFieldObject, "cursos": TriggerFieldList}
	for key, want := range wantTypes {
		if f := fieldByKey(spec, key); f == nil || f.Type != want || f.Required {
			t.Fatalf("%s = %+v, want optional %s", key, f, want)
		}
	}
	if f := fieldByKey(spec, "endereco"); !reflect.DeepEqual(f.ReadBy, []string{"n2"}) {
		t.Fatalf("a nested read must count for the field, got %v", f.ReadBy)
	}
	agent := fieldByKey(spec, "agent.name")
	if agent == nil || agent.Type != TriggerFieldText || agent.Required || agent.Example != DefaultTestAgentName || !reflect.DeepEqual(agent.ReadBy, []string{"n1"}) {
		t.Fatalf("agent.name = %+v", agent)
	}
	if !reflect.DeepEqual(spec.Issues, []TriggerInputIssue{{Code: IssueReadNotDeclared, Key: "matricula"}}) {
		t.Fatalf("issues = %+v, want the undeclared read", spec.Issues)
	}
	if !reflect.DeepEqual(spec.ReadKeys, []string{"agent.name", "cpf", "endereco", "matricula"}) {
		t.Fatalf("readKeys = %v", spec.ReadKeys)
	}
}

func TestAnAgentCallTriggerWithUnreadableInputsIsNotReadable(t *testing.T) {
	trigger := Node{ID: "t", Type: NodeTypeTriggerAgentCall, Config: map[string]interface{}{AgentCallInputsKey: "cpf"}}
	if spec := onlySpec(t, triggerGraph(trigger), TriggerInputSources{}); spec.Readable() {
		t.Fatalf("spec = %+v, an unreadable declaration must not be offered as an empty form", spec)
	}
}

func TestTheWebhookSpecBuildsASampleBodyFromWhatTheFlowReads(t *testing.T) {
	trigger := Node{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{WebhookEventsKey: []interface{}{"Matricula.Criada", "x"}}}
	g := triggerGraph(trigger, sendText("n1", "{{webhook.body.cliente.nome}} {{webhook.body.curso}} {{webhook.method}}"), sendText("n2", "{{webhook.body.cliente.cpf}}"))
	spec := onlySpec(t, g, TriggerInputSources{})

	if spec.Shape != TriggerInputJSON || len(spec.Fields) != 0 {
		t.Fatalf("spec = %+v", spec)
	}
	want := map[string]interface{}{"cliente": map[string]interface{}{"nome": "", "cpf": ""}, "curso": "", "event": "matricula.criada"}
	if !reflect.DeepEqual(spec.Sample, want) {
		t.Fatalf("sample = %v, want %v", spec.Sample, want)
	}
	if !reflect.DeepEqual(spec.ReadKeys, []string{"cliente.cpf", "cliente.nome", "curso"}) {
		t.Fatalf("readKeys = %v", spec.ReadKeys)
	}
}

func TestTheFormSpecUsesTheQuestionsKeyedAsProductionKeysTheAnswers(t *testing.T) {
	form := TriggerForm{ID: "f1", Name: "Inscrição", Questions: []TriggerFormQuestion{
		{Key: "nome", Prompt: "Seu nome", Required: true},
		{Key: "turno", Prompt: "Turno", Choices: []string{"Manhã", "Noite"}},
	}}
	single := Node{ID: "t", Type: NodeTypeTriggerFormSubmitted, Config: map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f1"}}}
	spec := onlySpec(t, triggerGraph(single, sendText("n1", "Oi {{form.nome}} {{form.idade}}")), TriggerInputSources{Forms: map[string]TriggerForm{"f1": form}})

	if spec.Shape != TriggerInputFields || fieldByKey(spec, DataKeyFormID) != nil {
		t.Fatalf("a single form needs no form choice: %+v", spec)
	}
	nome := fieldByKey(spec, "form.nome")
	if nome == nil || !nome.Required || nome.Description != "Seu nome" || !reflect.DeepEqual(nome.ReadBy, []string{"n1"}) {
		t.Fatalf("form.nome = %+v", nome)
	}
	turno := fieldByKey(spec, "form.turno")
	if turno == nil || !reflect.DeepEqual(turno.Options, []TriggerInputOption{{Value: "Manhã", Label: "Manhã"}, {Value: "Noite", Label: "Noite"}}) {
		t.Fatalf("form.turno = %+v", turno)
	}
	if !reflect.DeepEqual(spec.Issues, []TriggerInputIssue{{Code: IssueReadNotDeclared, Key: "form.idade"}}) {
		t.Fatalf("issues = %+v", spec.Issues)
	}

	anyForm := Node{ID: "t", Type: NodeTypeTriggerFormSubmitted, Config: map[string]interface{}{}}
	choices := []TriggerInputOption{{Value: "f1", Label: "Inscrição"}, {Value: "f2", Label: "Contato"}}
	spec = onlySpec(t, triggerGraph(anyForm, sendText("n1", "{{form.nome}}")), TriggerInputSources{FormChoices: choices})
	formID := fieldByKey(spec, DataKeyFormID)
	if formID == nil || !formID.Required || !reflect.DeepEqual(formID.Options, choices) {
		t.Fatalf("any form needs the form choice: %+v", spec)
	}
	if f := fieldByKey(spec, "form.nome"); f == nil || f.Required || !reflect.DeepEqual(f.ReadBy, []string{"n1"}) {
		t.Fatalf("an answer the flow reads becomes an optional field: %+v", f)
	}
	if len(spec.Issues) != 0 {
		t.Fatalf("issues = %+v", spec.Issues)
	}
}

func TestAFormTriggerWhoseFormCouldNotBeReadIsNotReadable(t *testing.T) {
	single := Node{ID: "t", Type: NodeTypeTriggerFormSubmitted, Config: map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f1"}}}
	cases := map[string]TriggerInputSources{
		"form missing":       {},
		"source read failed": {Forms: map[string]TriggerForm{"f1": {ID: "f1"}}, Unreadable: map[string]bool{"t": true}},
	}
	for name, sources := range cases {
		if spec := onlySpec(t, triggerGraph(single), sources); spec.Readable() {
			t.Fatalf("%s: spec = %+v", name, spec)
		}
	}
}

func TestTheOtherTriggerKindsDescribeWhatTheyCarry(t *testing.T) {
	stages := []TriggerInputOption{{Value: "s1", Label: "Novo"}, {Value: "s2", Label: "Matriculado"}}
	cases := []struct {
		name    string
		trigger Node
		reader  string
		shape   TriggerInputShape
		fields  []string
		options []TriggerInputOption
	}{
		{"any stage", Node{ID: "t", Type: NodeTypeTriggerStageAdded}, "{{stage_id}}", TriggerInputFields, []string{DataKeyStageID}, stages},
		{"one stage", Node{ID: "t", Type: NodeTypeTriggerStageAdded, Config: map[string]interface{}{"stage_id": "s2"}}, "", TriggerInputFields, []string{DataKeyStageID}, stages[1:]},
		{"birthday", Node{ID: "t", Type: NodeTypeTriggerBirthday}, "{{birthday}}", TriggerInputFields, []string{DataKeyBirthday}, nil},
		{"message received", Node{ID: "t", Type: NodeTypeTriggerMessageReceived}, "{{message}}", TriggerInputMessage, nil, nil},
		{"first message", Node{ID: "t", Type: NodeTypeTriggerFirstMessage}, "", TriggerInputMessage, nil, nil},
		{"call received", Node{ID: "t", Type: NodeTypeTriggerCallReceived}, "", TriggerInputNone, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := onlySpec(t, triggerGraph(tc.trigger, sendText("n1", tc.reader)), TriggerInputSources{Stages: stages})
			if spec.Shape != tc.shape || !spec.Readable() || spec.Sample != nil {
				t.Fatalf("spec = %+v", spec)
			}
			var keys []string
			for _, f := range spec.Fields {
				keys = append(keys, f.Key)
			}
			if !reflect.DeepEqual(keys, tc.fields) {
				t.Fatalf("fields = %v, want %v", keys, tc.fields)
			}
			if tc.options != nil && !reflect.DeepEqual(spec.Fields[0].Options, tc.options) {
				t.Fatalf("options = %v, want %v", spec.Fields[0].Options, tc.options)
			}
		})
	}
}

func TestEveryTriggerNodeGetsASpecPrimaryFirst(t *testing.T) {
	g := &Graph{Nodes: []Node{
		{ID: "a", Type: NodeTypeTriggerAgentCall},
		{ID: "x", Type: NodeTypeActionSendText},
		{ID: "w", Type: NodeTypeTriggerWebhook},
	}}
	specs := TriggerInputSpecsOf(g, TriggerInputSources{})
	if len(specs) != 2 || specs[0].NodeID != "a" || specs[1].NodeID != "w" {
		t.Fatalf("specs = %+v", specs)
	}
	encoded, _ := json.Marshal(specs[1])
	for _, want := range []string{`"fields":[]`, `"readKeys":[]`, `"issues":[]`, `"label":""`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("%s missing from %s", want, encoded)
		}
	}
}

func TestTriggerInputValidationUsesTheProductionRules(t *testing.T) {
	agent := onlySpec(t, triggerGraph(agentCallTestTrigger()), TriggerInputSources{})
	webhook := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{WebhookEventsKey: []interface{}{"matricula.criada"}}}), TriggerInputSources{})
	stage := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerStageAdded}), TriggerInputSources{Stages: []TriggerInputOption{{Value: "s1", Label: "Novo"}}})
	birthday := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerBirthday}), TriggerInputSources{})
	huge := strings.Repeat("a", tracepayload.MaxBytes+1)
	cases := []struct {
		name  string
		spec  TriggerInputSpec
		input map[string]interface{}
		want  []TriggerInputIssue
	}{
		{"complete agent call", agent, map[string]interface{}{"cpf": "529", "idade": 19.0, "aceite": true, "endereco": map[string]interface{}{}, "cursos": []interface{}{"ADS"}, "agent.name": "Bia"}, nil},
		{"optional field left empty", agent, map[string]interface{}{"cpf": "529", "idade": nil}, nil},
		{"no input at all refuses the required field", agent, nil, []TriggerInputIssue{{IssueRequired, "cpf"}}},
		{"a blank required text is missing", agent, map[string]interface{}{"cpf": "  "}, []TriggerInputIssue{{IssueRequired, "cpf"}}},
		{"wrong types and an unknown field", agent, map[string]interface{}{"cpf": 529.0, "idade": "dezenove", "cursos": "ADS", "nome": "Ana"}, []TriggerInputIssue{{IssueWrongType, "cpf"}, {IssueWrongType, "cursos"}, {IssueWrongType, "idade"}, {IssueUnknownField, "nome"}}},
		{"an input over the cap", agent, map[string]interface{}{"cpf": huge}, []TriggerInputIssue{{IssueTooLarge, ""}}},
		{"an accepted webhook event", webhook, map[string]interface{}{"event": " Matricula.Criada "}, nil},
		{"a filtered webhook event", webhook, map[string]interface{}{"event": "outro"}, []TriggerInputIssue{{IssueEventNotAccepted, "event"}}},
		{"a webhook body without event", webhook, map[string]interface{}{}, []TriggerInputIssue{{IssueEventNotAccepted, "event"}}},
		{"a known stage", stage, map[string]interface{}{"stage_id": "s1"}, nil},
		{"a stage of another workspace", stage, map[string]interface{}{"stage_id": "s9"}, []TriggerInputIssue{{IssueUnknownOption, "stage_id"}}},
		{"a birthday that is not a date", birthday, map[string]interface{}{"birthday": "22/10"}, []TriggerInputIssue{{IssueWrongType, "birthday"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateTriggerInput(tc.spec, tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("issues = %+v, want %+v", got, tc.want)
			}
		})
	}
}

var testSubject = TriggerTestSubject{RunID: "run-1", EntryID: "entry-1", ContactNumber: "5511999990000", LeadID: "lead-1", LeadName: "Contato de teste", Today: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)}

func TestTheTesterSeedsTheRunWithTheProductionBuilders(t *testing.T) {
	agent := onlySpec(t, triggerGraph(agentCallTestTrigger()), TriggerInputSources{})
	webhookLead := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{WebhookStartOnKey: "lead"}}), TriggerInputSources{})
	form := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerFormSubmitted, Config: map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f1"}}}),
		TriggerInputSources{Forms: map[string]TriggerForm{"f1": {ID: "f1", Questions: []TriggerFormQuestion{{Key: "nome"}}}}})
	stage := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerStageAdded}), TriggerInputSources{Stages: []TriggerInputOption{{Value: "s1"}}})
	birthday := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerBirthday}), TriggerInputSources{})
	call := onlySpec(t, &Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerCallReceived}}}, TriggerInputSources{})
	message := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerMessageReceived}), TriggerInputSources{})
	cases := []struct {
		name  string
		spec  TriggerInputSpec
		input map[string]interface{}
		want  map[string]interface{}
	}{
		{"agent call", agent, map[string]interface{}{"cpf": "529", "idade": nil}, AgentCallRunData(map[string]interface{}{"cpf": "529"}, "", DefaultTestAgentName)},
		{"agent call with a name", agent, map[string]interface{}{"cpf": "529", "agent.name": "Bia"}, AgentCallRunData(map[string]interface{}{"cpf": "529"}, "", "Bia")},
		{"webhook on a lead", webhookLead, map[string]interface{}{"phone": "11"}, LeadSubject{LeadID: "lead-1", LeadName: "Contato de teste"}.RunData(WebhookRunData(TestWebhookMethod, map[string]interface{}{"phone": "11"}, nil))},
		{"webhook without a body", onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerWebhook}), TriggerInputSources{}), nil, WebhookRunData(TestWebhookMethod, map[string]interface{}{}, nil)},
		{"form", form, map[string]interface{}{"form.nome": "Ana"}, FormSubmittedRunData("f1", "run-1", map[string]interface{}{"nome": "Ana"})},
		{"stage", stage, map[string]interface{}{"stage_id": "s1"}, StageAddedRunData("s1")},
		{"birthday defaults to today", birthday, nil, LeadSubject{LeadID: "lead-1", LeadName: "Contato de teste"}.RunData(BirthdayRunData(testSubject.Today))},
		{"birthday on a given day", birthday, map[string]interface{}{"birthday": "2026-01-05"}, LeadSubject{LeadID: "lead-1", LeadName: "Contato de teste"}.RunData(BirthdayRunData(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)))},
		{"a call ignores the input", call, map[string]interface{}{"x": 1.0}, map[string]interface{}{}},
		{"message waits for the first message", message, map[string]interface{}{"x": 1.0}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, issues := TriggerRunDataFor(tc.spec, tc.input, testSubject)
			if len(issues) != 0 {
				t.Fatalf("issues = %+v", issues)
			}
			if encodedRunData(t, data) != encodedRunData(t, tc.want) {
				t.Fatalf("data = %v\nwant %v", data, tc.want)
			}
		})
	}
	if data, issues := TriggerRunDataFor(agent, nil, testSubject); data != nil || len(issues) != 1 {
		t.Fatalf("an invalid input must not seed anything: %v %v", data, issues)
	}
}

func TestARecordedRunMapsBackToTheSpecInputWithItsMaskedKeys(t *testing.T) {
	agent := onlySpec(t, triggerGraph(agentCallTestTrigger()), TriggerInputSources{})
	webhook := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerWebhook}), TriggerInputSources{})
	form := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerFormSubmitted}), TriggerInputSources{FormChoices: []TriggerInputOption{{Value: "f1"}}})
	message := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerMessageReceived}), TriggerInputSources{})
	cases := []struct {
		name   string
		spec   TriggerInputSpec
		data   map[string]interface{}
		input  map[string]interface{}
		masked []string
	}{
		{"agent call", agent,
			map[string]interface{}{"input": map[string]interface{}{"cpf": "•••.•••.•••-25", "idade": 19.0, "removido": "x"}, "agent": map[string]interface{}{"id": "a1", "name": "Bia"}},
			map[string]interface{}{"cpf": "•••.•••.•••-25", "idade": 19.0, "agent.name": "Bia"}, []string{"cpf"}},
		{"webhook", webhook,
			map[string]interface{}{"webhook": map[string]interface{}{"method": "POST", "body": map[string]interface{}{"cliente": map[string]interface{}{"token": "••••", "nome": "Ana"}}}, "lead_id": "l1"},
			map[string]interface{}{"cliente": map[string]interface{}{"token": "••••", "nome": "Ana"}}, []string{"cliente.token"}},
		{"webhook with a text body", webhook, map[string]interface{}{"webhook": map[string]interface{}{"body": "plain"}}, map[string]interface{}{}, []string{}},
		{"form", form,
			map[string]interface{}{"form_id": "f1", "form_response_id": "r1", "form.senha": "••••"},
			map[string]interface{}{"form_id": "f1"}, []string{}},
		{"message", message, map[string]interface{}{"message": "oi", "message_type": "text"}, map[string]interface{}{"message": "oi"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := TriggerInputFromRunData(tc.spec, tc.data)
			if !reflect.DeepEqual(input, tc.input) {
				t.Fatalf("input = %v, want %v", input, tc.input)
			}
			if masked := MaskedTriggerInputKeys(tc.spec, input); !reflect.DeepEqual(masked, tc.masked) {
				t.Fatalf("masked = %v, want %v", masked, tc.masked)
			}
		})
	}
}

func TestThePathToANodeStartsAtThePrimaryTriggerElseTheFirstTriggerThatReachesIt(t *testing.T) {
	g := &Graph{
		Nodes: []Node{{ID: "a", Type: NodeTypeTriggerAgentCall}, {ID: "w", Type: NodeTypeTriggerWebhook}, {ID: "m", Type: NodeTypeTriggerMessageReceived}, {ID: "x", Type: NodeTypeActionSetVariable}, {ID: "y", Type: NodeTypeActionSetVariable}, {ID: "z", Type: NodeTypeActionSetVariable}},
		Edges: []Edge{{Source: "a", Target: "x"}, {Source: "w", Target: "x"}, {Source: "w", Target: "y"}, {Source: "m", Target: "y"}},
	}
	cases := map[string]string{"x": "a", "y": "w", "w": "w", "z": ""}
	for target, want := range cases {
		got := ""
		if n := PathStartTrigger(g, target); n != nil {
			got = n.ID
		}
		if got != want {
			t.Fatalf("PathStartTrigger(%s) = %q, want %q", target, got, want)
		}
	}
}
