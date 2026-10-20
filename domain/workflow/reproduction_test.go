package workflow

import (
	"errors"
	"testing"
)

func reproductionDraft() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerWebhook},
			{ID: "login", Type: NodeTypeActionHTTPRequest},
			{ID: "calc", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "return {a: 1}"}},
			{ID: "send", Type: NodeTypeActionSendText},
			{ID: "set", Type: NodeTypeActionSetVariable},
			{ID: "new", Type: NodeTypeActionHTTPRequest},
			{ID: "end", Type: NodeTypeEnd},
		},
	}
}

func TestReproductionPinsReplayTheRecordedSideEffectsOnly(t *testing.T) {
	steps := []*WorkflowRunLog{
		{NodeID: "t", NodeType: NodeTypeTriggerWebhook, Status: LogStatusExecuted},
		{NodeID: "login", Status: LogStatusFailed, Output: map[string]interface{}{"attempt": 1.0}},
		{NodeID: "login", Status: LogStatusExecuted, Output: map[string]interface{}{"attempt": 2.0}, Branch: "success"},
		{NodeID: "calc", Status: LogStatusExecuted, Output: map[string]interface{}{"a": 1.0}},
		{NodeID: "send", Status: LogStatusExecuted, Output: map[string]interface{}{"sent": true}},
		{NodeID: "set", Status: LogStatusExecuted, Output: map[string]interface{}{"v": 1.0}},
		{NodeID: "gone", Status: LogStatusExecuted, Output: map[string]interface{}{}},
	}
	draft := reproductionDraft()
	pins := ReproductionPins(&draft, steps, "run-9", pinTime, TestBehaviorsOf([]NodeDefinition{
		{Type: NodeTypeActionHTTPRequest, TestBehavior: NodeTestExternalCall},
		{Type: NodeTypeActionCode, TestBehavior: NodeTestExternalCall},
		{Type: NodeTypeActionSendText, TestBehavior: NodeTestSendsMessage},
		{Type: NodeTypeActionSetVariable, TestBehavior: NodeTestPure},
	}))

	if got := pins["login"]; got.Output["attempt"] != 2.0 || got.Edge != "success" || got.Source != PinSourceOfRun("run-9") {
		t.Fatalf("login pin = %+v, want the last usable attempt with its branch", got)
	}
	for _, id := range []string{"calc", "send"} {
		if _, ok := pins[id]; !ok {
			t.Fatalf("%s must be pinned: code and sends are replayed from the run", id)
		}
	}
	for _, id := range []string{"t", "set", "new", "gone", "end"} {
		if _, ok := pins[id]; ok {
			t.Fatalf("%s must not be pinned (no side effect, not reached, or not in the draft)", id)
		}
	}
}

func TestReproductionSeedPrefersStoredTriggerData(t *testing.T) {
	steps := []*WorkflowRunLog{{NodeID: "t", NodeType: NodeTypeTriggerWebhook, Status: LogStatusExecuted, Output: map[string]interface{}{"from": "step"}}}
	cases := []struct {
		name    string
		run     WorkflowRun
		steps   []*WorkflowRunLog
		want    string
		wantErr error
	}{
		{"stored trigger data", WorkflowRun{TriggerData: map[string]interface{}{"from": "column"}}, steps, "column", nil},
		{"older run seeds from the trigger step", WorkflowRun{}, steps, "step", nil},
		{"older run without a trigger output is refused", WorkflowRun{}, []*WorkflowRunLog{{NodeID: "t", NodeType: NodeTypeTriggerWebhook, Status: LogStatusExecuted}}, "", ErrNoTriggerData},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed, err := ReproductionSeed(&tc.run, tc.steps)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && seed["from"] != tc.want {
				t.Fatalf("seed = %v, want from=%s", seed, tc.want)
			}
		})
	}
}

func TestTriggerDataIsRedactedAndCapped(t *testing.T) {
	state := NewRunState()
	state.Set("cpf", "123.456.789-09")
	state.Set("api_token", "abc")
	state.Set("name", "Ana")
	data := TriggerDataOf(state)
	if data["name"] != "Ana" {
		t.Fatalf("plain values stay readable, got %v", data["name"])
	}
	if data["api_token"] == "abc" || data["cpf"] == "123.456.789-09" {
		t.Fatalf("secrets and documents must be masked before storing, got %v", data)
	}
	if v, _ := state.Get("api_token"); v != "abc" {
		t.Fatal("capturing trigger data must not change the run state")
	}
	if TriggerDataOf(NewRunState()) != nil {
		t.Fatal("an empty start stores nothing")
	}
}
