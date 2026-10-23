package workflow_usecase

import (
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/workflow"
)

func menuWithChoices(edges ...workflow.Edge) *workflow.Workflow {
	wf := replyMenuWorkflow()
	wf.Graph.Nodes[1].Config = map[string]interface{}{
		"interactive_type": "buttons",
		"buttons":          `[{"Type":"reply","ID":"sim","Title":"Sim"},{"Type":"reply","ID":"nao","Title":"Não"}]`,
	}
	wf.Graph.Nodes = append(wf.Graph.Nodes, workflow.Node{ID: "after", Type: workflow.NodeTypeEnd})
	wf.Graph.Edges = append(wf.Graph.Edges, edges...)
	return wf
}

func wakeWithChoices(t *testing.T, wf *workflow.Workflow, promptKey string, data map[string]interface{}) *spyChoiceRecorder {
	t.Helper()
	spy := &spyChoiceRecorder{}
	run := waitingMenuRun()
	run.State.Set(workflow.StateKeyChoicePrompt, promptKey)
	runs := &recordingRunRepo{waitingReply: run}
	engine := NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry())
	engine.SetChoiceRecorder(spy)
	waiters := NewReplyWaiters(stubWorkflowRepo{byID: map[string]*workflow.Workflow{wf.ID: wf}}, runs, engine)
	event := messageEvent()
	event.Data = data
	waiters.WakeForReply(run, event)
	return spy
}

func TestAReplyToAWaitingPromptIsRecordedAsTheEngineRoutesIt(t *testing.T) {
	wired := []workflow.Edge{{Source: "menu", Target: "after", Label: "sim"}, {Source: "menu", Target: "after", Label: "no_match"}}
	cases := []struct {
		name   string
		wf     *workflow.Workflow
		key    string
		data   map[string]interface{}
		option string
	}{
		{"a tap on a shown option is a click on it", menuWithChoices(wired...), "wf:run-woken:menu#1", map[string]interface{}{"selected_option_id": "sim"}, "sim"},
		{"a tap on an unwired option is still a click on it", menuWithChoices(wired...), "wf:run-woken:menu#1", map[string]interface{}{"selected_option_id": "nao"}, "nao"},
		{"typed text that reads like a title is free text", menuWithChoices(wired...), "wf:run-woken:menu#1", map[string]interface{}{"message": "Sim"}, choicestats.OptionFreeText},
		{"a reply the engine cannot route is not counted", menuWithChoices(workflow.Edge{Source: "menu", Target: "after", Label: "sim"}), "wf:run-woken:menu#1", map[string]interface{}{"message": "oi"}, ""},
		{"a prompt that was never recorded has nothing to click", menuWithChoices(wired...), "", map[string]interface{}{"selected_option_id": "sim"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := wakeWithChoices(t, tc.wf, tc.key, tc.data)
			if tc.option == "" {
				if len(spy.clicks) != 0 {
					t.Fatalf("clicks = %+v", spy.clicks)
				}
				return
			}
			if len(spy.clicks) != 1 {
				t.Fatalf("clicks = %+v", spy.clicks)
			}
			click := spy.clicks[0]
			if click.PromptKey != tc.key || click.Option != tc.option || click.WorkspaceID != "ws-1" || click.At.IsZero() {
				t.Fatalf("click = %+v", click)
			}
		})
	}
}
