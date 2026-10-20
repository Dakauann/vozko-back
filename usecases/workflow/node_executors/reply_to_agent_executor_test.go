package node_executors

import (
	"reflect"
	"testing"

	"vozko/domain/redact"
	"vozko/domain/workflow"
)

func replyToAgentCtx(withAgentTrigger bool, fields []interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	state.Set("_last_status", "aprovado")
	state.Set("cpf_lido", "52998224725")
	nodes := []workflow.Node{{ID: "r", Type: workflow.NodeTypeActionReplyToAgent, Config: map[string]interface{}{workflow.ReplyToAgentFieldsKey: fields}}, {ID: "end", Type: workflow.NodeTypeEnd}}
	if withAgentTrigger {
		nodes = append(nodes, workflow.Node{ID: "t", Type: workflow.NodeTypeTriggerAgentCall})
	} else {
		nodes = append(nodes, workflow.Node{ID: "t", Type: workflow.NodeTypeTriggerWebhook})
	}
	graph := &workflow.Graph{Nodes: nodes, Edges: []workflow.Edge{{Source: "t", Target: "r"}, {Source: "r", Target: "end"}}}
	return &workflow.NodeContext{
		Run:   &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1", AgentResult: map[string]interface{}{"antigo": "x"}},
		Node:  &graph.Nodes[0],
		Graph: graph,
		State: &state,
	}
}

func replyFields() []interface{} {
	return []interface{}{
		map[string]interface{}{"key": "status", "value": "{{last.status}}"},
		map[string]interface{}{"key": "cpf", "value": "{{cpf_lido}}"},
		map[string]interface{}{"key": "access_token", "value": "abc123"},
	}
}

func TestReplyToAgentStoresTheRedactedFieldsAsTheRunResultAndContinues(t *testing.T) {
	ctx := replyToAgentCtx(true, replyFields())

	result, err := NewReplyToAgentExecutor().Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"status": "aprovado", "cpf": "•••••••••25", "access_token": redact.Mask}
	if !reflect.DeepEqual(ctx.Run.AgentResult, want) {
		t.Fatalf("agent result = %#v, want %#v (the latest write replaces the previous one)", ctx.Run.AgentResult, want)
	}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("output = %#v, want the same map for templates", result.Output)
	}
	if result.NextNodeID != "end" || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestReplyToAgentRefusesAWorkflowWithoutTheAgentCallTrigger(t *testing.T) {
	ctx := replyToAgentCtx(false, replyFields())

	result, err := NewReplyToAgentExecutor().Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" || result.ErrorKind != workflow.ErrorKindConfig {
		t.Fatalf("result = %+v, want a config refusal", result)
	}
	if ctx.Run.AgentResult["antigo"] != "x" || len(ctx.Run.AgentResult) != 1 {
		t.Fatalf("a refused node must not write the result: %#v", ctx.Run.AgentResult)
	}
}

func TestReplyToAgentIsAMessagingNodeWithMappedFields(t *testing.T) {
	def := NewReplyToAgentExecutor().Definition()
	if def.Type != workflow.NodeTypeActionReplyToAgent || def.Label != "Responder ao agente" {
		t.Fatalf("definition = %+v", def)
	}
	if !workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeMessages) || workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeVoice) {
		t.Fatal("the reply node belongs to messaging workflows only")
	}
	if len(def.ConfigSchema) != 1 || def.ConfigSchema[0].Key != workflow.ReplyToAgentFieldsKey {
		t.Fatalf("config schema = %+v", def.ConfigSchema)
	}
}
