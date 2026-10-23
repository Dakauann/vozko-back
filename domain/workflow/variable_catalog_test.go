package workflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func catalogDefs() []NodeDefinition {
	return []NodeDefinition{
		{Type: NodeTypeActionHTTPRequest, Label: "Requisição HTTP", OutputKeys: []OutputKeyDefinition{{Key: "status_code", Label: "Código HTTP", Type: VariableTypeNumber}, {Key: "body", Label: "Corpo da resposta", Type: VariableTypeText}, {Key: "success", Label: "Deu certo", Type: VariableTypeBoolean}, {Key: "*"}}},
		{Type: NodeTypeConditionBranch, Label: "Condição", OutputKeys: []OutputKeyDefinition{{Key: "matched", Label: "Condição atendida", Type: VariableTypeBoolean}, {Key: "variable", RepeatsConfig: true}, {Key: "operator", RepeatsConfig: true}}},
		{Type: NodeTypeActionSetVariable, Label: "Definir variável", OutputKeys: []OutputKeyDefinition{{Key: "variable", RepeatsConfig: true}, {Key: "value", RepeatsConfig: true}}},
		{Type: NodeTypeActionSendText, Label: "Enviar Texto", OutputKeys: []OutputKeyDefinition{{Key: "text", Label: "Texto enviado", Type: VariableTypeText}, {Key: "sent", Label: "Enviada", Type: VariableTypeBoolean}, {Key: "message_id", Label: "ID da mensagem", Type: VariableTypeText}}},
		{Type: NodeTypeWaitForReply, Label: "Aguardar resposta", OutputKeys: []OutputKeyDefinition{{Key: "waiting", Label: "Aguardando", Type: VariableTypeBoolean}}},
		{Type: NodeTypeActionCode, Label: "Código", OutputKeys: []OutputKeyDefinition{{Key: "*"}}},
		{Type: NodeTypeActionReplyToAgent, Label: "Responder ao agente", OutputKeys: []OutputKeyDefinition{{Key: "<campo>"}}},
		{Type: NodeTypeEnd, Label: "Fim"},
	}
}

func enrollmentGraph() *Graph {
	return &Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerAgentCall, Config: map[string]interface{}{
				"display_name": "Bia chama matrícula",
				AgentCallInputsKey: []interface{}{
					map[string]interface{}{"name": "cpf", "type": "string", "required": true, "example": "52998224725"},
					map[string]interface{}{"name": "endereco", "type": "object"},
				},
			}},
			{ID: "h", Type: NodeTypeActionHTTPRequest, Config: map[string]interface{}{"capture_variable": "cadastro", "display_name": "Busca cadastro"}},
			{ID: "c", Type: NodeTypeConditionBranch, Config: map[string]interface{}{}},
			{ID: "sv", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "status", "value": "ok"}},
			{ID: "s", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "Oi"}},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{Source: "t", Target: "h"},
			{Source: "h", Target: "c", Label: "sucesso"},
			{Source: "c", Target: "sv", Label: "true"},
			{Source: "c", Target: "s", Label: "false"},
			{Source: "sv", Target: "s"},
			{Source: "s", Target: "end"},
		},
	}
}

func catalogAt(t *testing.T, g *Graph, sources VariableSources, nodeID string) []CatalogVariable {
	t.Helper()
	sources.Specs = TriggerInputSpecsOf(g, TriggerInputSources{Forms: sources.Forms})
	return VariableCatalogAt(g, catalogDefs(), sources, nodeID).Variables
}

func variableByRef(vars []CatalogVariable, ref string) *CatalogVariable {
	for i := range vars {
		if vars[i].Ref == ref {
			return &vars[i]
		}
	}
	return nil
}

func TestTheCatalogNamesTheAgentFieldsAsAlwaysAvailableTriggerData(t *testing.T) {
	vars := catalogAt(t, enrollmentGraph(), VariableSources{}, "s")

	cpf := variableByRef(vars, "input.cpf")
	example := "52998224725"
	want := CatalogVariable{
		Ref: "input.cpf", Label: "Enviado pelo agente › cpf", Group: VariableGroupTrigger, Kind: VariableKindTrigger,
		Source: &VariableSource{NodeID: "t", NodeName: "Bia chama matrícula"}, Type: VariableTypeText, Example: &example, Availability: AvailableAlways,
	}
	if cpf == nil || !reflect.DeepEqual(*cpf, want) {
		t.Fatalf("input.cpf = %+v, want %+v", cpf, want)
	}
	if endereco := variableByRef(vars, "input.endereco"); endereco == nil || endereco.Type != VariableTypeObject || endereco.Example != nil {
		t.Fatalf("input.endereco = %+v", endereco)
	}
	if agent := variableByRef(vars, "agent.name"); agent == nil || agent.Group != VariableGroupTrigger {
		t.Fatalf("agent.name = %+v", agent)
	}
}

func TestAVariableSetOnOnlyOneBranchIsAvailableOnSomePaths(t *testing.T) {
	vars := catalogAt(t, enrollmentGraph(), VariableSources{}, "s")

	status := variableByRef(vars, "status")
	if status == nil || status.Availability != AvailableOnSomePaths || status.Kind != VariableKindNode || status.Group != VariableGroupNodes ||
		status.Source == nil || status.Source.NodeID != "sv" || status.Source.NodeName != "Definir variável" {
		t.Fatalf("status = %+v", status)
	}
	if capture := variableByRef(vars, "cadastro"); capture == nil || capture.Availability != AvailableAlways || capture.Type != VariableTypeObject || capture.Source.NodeName != "Busca cadastro" {
		t.Fatalf("cadastro = %+v", capture)
	}
	if out := variableByRef(vars, "node.h.status_code"); out == nil || out.Label != "Busca cadastro › Código HTTP" || out.Type != VariableTypeNumber {
		t.Fatalf("node.h.status_code = %+v", out)
	}
}

func TestEachValueAppearsOnceUnderItsNodeAndTheLastStepAliasIsNeverOffered(t *testing.T) {
	g := enrollmentGraph()
	g.Nodes = append(g.Nodes[:5], Node{ID: "s2", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "Tchau"}}, g.Nodes[5])
	g.Edges[5] = Edge{Source: "s", Target: "s2"}
	g.Edges = append(g.Edges, Edge{Source: "s2", Target: "end"})

	vars := catalogAt(t, g, VariableSources{}, "end")

	seen := map[string]bool{}
	for _, v := range vars {
		if seen[v.Ref] {
			t.Fatalf("ref %q listed twice", v.Ref)
		}
		seen[v.Ref] = true
	}
	for _, v := range vars {
		if strings.HasPrefix(v.Ref, "last.") {
			t.Fatalf("%s is offered, but last.* only repeats a value already listed under its node", v.Ref)
		}
	}
	if variableByRef(vars, "node.s.text") == nil || variableByRef(vars, "node.s2.text") == nil {
		t.Fatal("each send keeps its own node ref")
	}
}

func TestTheCatalogOrdersTriggerNodesByDistanceThenContactSystemAndSecrets(t *testing.T) {
	vars := catalogAt(t, enrollmentGraph(), VariableSources{Secrets: []string{"api_key"}}, "s")

	rank := map[VariableGroup]int{VariableGroupTrigger: 0, VariableGroupNodes: 1, VariableGroupContact: 2, VariableGroupSystem: 3, VariableGroupSecrets: 4}
	for i := 1; i < len(vars); i++ {
		if rank[vars[i-1].Group] > rank[vars[i].Group] {
			t.Fatalf("%s (%s) listed before %s (%s)", vars[i-1].Ref, vars[i-1].Group, vars[i].Ref, vars[i].Group)
		}
	}
	firstNode := -1
	for i, v := range vars {
		if v.Group == VariableGroupNodes {
			firstNode = i
			break
		}
	}
	if firstNode < 0 || vars[firstNode].Source.NodeID != "c" {
		t.Fatalf("first node entry = %+v, want the nearest node first", vars[firstNode])
	}
	date := variableByRef(vars, "sys.date")
	if date == nil || date.Kind != VariableKindDefault || date.Group != VariableGroupSystem || date.Availability != AvailableAlways || date.Source != nil || date.Type != VariableTypeDate {
		t.Fatalf("sys.date = %+v", date)
	}
	secret := vars[len(vars)-1]
	if secret.Ref != "secret.api_key" || secret.Kind != VariableKindSecret || secret.Group != VariableGroupSecrets || secret.Example != nil {
		t.Fatalf("last entry = %+v", secret)
	}
}

func TestAMessageTriggerProvidesTheContactAndConversation(t *testing.T) {
	g := triggerGraph(Node{ID: "m", Type: NodeTypeTriggerMessageReceived}, sendText("s", "Oi"))
	vars := catalogAt(t, g, VariableSources{}, "s")

	for _, ref := range []string{"message", "contact_number"} {
		v := variableByRef(vars, ref)
		if v == nil || v.Group != VariableGroupContact || v.Kind != VariableKindSystem || v.Availability != AvailableAlways || v.Source == nil || v.Source.NodeID != "m" {
			t.Fatalf("%s = %+v", ref, v)
		}
	}
}

func TestAReplyIsOnlyAvailableAfterTheRepliedOutput(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerWebhook},
			{ID: "w", Type: NodeTypeWaitForReply, Config: map[string]interface{}{}},
			{ID: "ok", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "a"}},
			{ID: "late", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "b"}},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{Source: "t", Target: "w"},
			{Source: "w", Target: "ok", Label: "replied"},
			{Source: "w", Target: "late", Label: "timeout"},
			{Source: "ok", Target: "end"},
			{Source: "late", Target: "end"},
		},
	}
	if v := variableByRef(catalogAt(t, g, VariableSources{}, "ok"), "message"); v == nil || v.Availability != AvailableAlways || v.Source.NodeID != "w" {
		t.Fatalf("message after a reply = %+v", v)
	}
	if v := variableByRef(catalogAt(t, g, VariableSources{}, "late"), "message"); v != nil {
		t.Fatalf("message after a timeout = %+v, want none", v)
	}
	if v := variableByRef(catalogAt(t, g, VariableSources{}, "end"), "message"); v == nil || v.Availability != AvailableOnSomePaths {
		t.Fatalf("message after the merge = %+v", v)
	}
}

func TestStaticCodeStateWritesAreListed(t *testing.T) {
	g := triggerGraph(Node{ID: "m", Type: NodeTypeTriggerMessageReceived},
		Node{ID: "js", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "state.set('protocolo', 1); state.set(\"etapa\", 'b'); return {}", "output_variable": "retorno"}},
		sendText("s", "x"))
	vars := catalogAt(t, g, VariableSources{}, "s")
	for _, ref := range []string{"protocolo", "etapa", "retorno"} {
		if v := variableByRef(vars, ref); v == nil || v.Source.NodeID != "js" || v.Availability != AvailableAlways {
			t.Fatalf("%s = %+v", ref, v)
		}
	}
}

func TestTheReplyToTheAgentListsItsFieldsAsOutputs(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(),
		Node{ID: "r", Type: NodeTypeActionReplyToAgent, Config: map[string]interface{}{ReplyToAgentFieldsKey: []interface{}{map[string]interface{}{"key": "status", "value": "ok"}}}},
		sendText("s", "x"))
	if v := variableByRef(catalogAt(t, g, VariableSources{}, "s"), "node.r.status"); v == nil {
		t.Fatal("node.r.status missing")
	}
}

func TestAFormAnswerIsAvailableAfterTheFormIsCompleted(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "m", Type: NodeTypeTriggerMessageReceived},
			{ID: "f", Type: NodeTypeActionSendForm, Config: map[string]interface{}{FormNodeFormIDKey: "form-1"}},
			{ID: "s", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "x"}},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{{Source: "m", Target: "f"}, {Source: "f", Target: "s", Label: FormHandleCompleted}, {Source: "f", Target: "end", Label: FormHandleNoReply}, {Source: "s", Target: "end"}},
	}
	forms := map[string]TriggerForm{"form-1": {ID: "form-1", Name: "Cadastro", Questions: []TriggerFormQuestion{{Key: "nome", Prompt: "Seu nome"}}}}
	v := variableByRef(catalogAt(t, g, VariableSources{Forms: forms}, "s"), "form.nome")
	if v == nil || v.Label != "Resposta do formulário › Seu nome" || v.Availability != AvailableAlways || v.Source.NodeID != "f" {
		t.Fatalf("form.nome = %+v", v)
	}
}

func TestANodeNoTriggerReachesSeesOnlyTheSystemValues(t *testing.T) {
	g := enrollmentGraph()
	g.Nodes = append(g.Nodes, Node{ID: "solto", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "x"}})
	for _, v := range catalogAt(t, g, VariableSources{}, "solto") {
		if v.Group != VariableGroupSystem && v.Group != VariableGroupSecrets {
			t.Fatalf("unexpected %+v", v)
		}
	}
}

func TestTheCatalogOfALongChainStillSeesTheFirstStep(t *testing.T) {
	g := &Graph{Nodes: []Node{{ID: "m", Type: NodeTypeTriggerMessageReceived}, {ID: "v0", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "inicio", "value": "x"}}}}
	g.Edges = append(g.Edges, Edge{Source: "m", Target: "v0"})
	prev := "v0"
	for i := 1; i < MaxNodesPerWorkflow-2; i++ {
		id := fmt.Sprintf("n%d", i)
		g.Nodes = append(g.Nodes, Node{ID: id, Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "x"}})
		g.Edges = append(g.Edges, Edge{Source: prev, Target: id}, Edge{Source: prev, Target: id, Label: "b"})
		prev = id
	}
	if v := variableByRef(catalogAt(t, g, VariableSources{}, prev), "inicio"); v == nil || v.Availability != AvailableAlways {
		t.Fatalf("inicio = %+v", v)
	}
}

func TestANodeOutputIsOfferedWithTheLabelAndTypeOfItsDefinition(t *testing.T) {
	g := enrollmentGraph()
	g.Nodes[2].Config["display_name"] = "Turno vazio?"

	vars := catalogAt(t, g, VariableSources{}, "s")

	matched := variableByRef(vars, "node.c.matched")
	if matched == nil || matched.Label != "Turno vazio? › Condição atendida" || matched.Type != VariableTypeBoolean {
		t.Fatalf("node.c.matched = %+v", matched)
	}
	for _, ref := range []string{"node.c.variable", "node.c.operator", "node.sv.variable", "node.sv.value"} {
		if v := variableByRef(vars, ref); v != nil {
			t.Fatalf("%s is offered, but it only repeats the node configuration", ref)
		}
	}
}
