package workflow

import (
	"reflect"
	"testing"

	"vozko/domain/choicestats"
)

func TestAWorkflowPromptIsKeyedByItsRunAndNodeVisit(t *testing.T) {
	version := 7
	run := &WorkflowRun{ID: "run-1", WorkflowID: "wf-1", CurrentNodeID: "ask", WorkflowVersion: &version, State: NewRunState()}
	run.State.Set(StateKeyDurableSteps, 3)
	if got := ChoicePromptKey(run); got != choicestats.WorkflowPromptKey("run-1", "ask#3") {
		t.Fatalf("key = %q", got)
	}
	run.RetryCount = 1
	run.State.Set(StateKeyDurableSteps, 4)
	if got := ChoicePromptKey(run); got != choicestats.WorkflowPromptKey("run-1", "ask#3") {
		t.Fatalf("a retried send must keep its key, got %q", got)
	}
	want := choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf-1", NodeID: "ask", Version: 7}
	if got := ChoicePromptSource(run, "ask", ""); got != want {
		t.Fatalf("source = %+v", got)
	}
	if got := ChoicePromptSource(&WorkflowRun{WorkflowID: "wf-1"}, "ask", "tpl-1"); got.Version != 0 || got.TemplateID != "tpl-1" {
		t.Fatalf("a run without a version counts as version 0: %+v", got)
	}
}

func TestTheCanvasListsEveryNodeThatCanSendAChoice(t *testing.T) {
	g := &Graph{Nodes: []Node{
		{ID: "t", Type: NodeTypeTriggerMessageReceived},
		{ID: "ask", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{
			"interactive_type": "buttons",
			"buttons":          `[{"Type":"reply","ID":"sim","Title":"Sim"},{"Type":"reply","ID":"nao","Title":"Não"},{"Type":"reply","ID":"sim","Title":"Dup"}]`,
		}},
		{ID: "tpl", Type: NodeTypeActionSendTemplate},
		{ID: "txt", Type: NodeTypeActionSendText},
	}}
	got := ChoiceCanvas(g)
	want := []choicestats.CanvasNode{
		{NodeID: "ask", Options: []choicestats.OfferedOption{{ID: "sim"}, {ID: "nao"}}},
		{NodeID: "tpl"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("canvas = %+v", got)
	}
	if ids := InteractiveOptionIDs(g.Nodes[1].Config); !reflect.DeepEqual(ids, []string{"sim", "nao"}) {
		t.Fatalf("ids = %v", ids)
	}
}
