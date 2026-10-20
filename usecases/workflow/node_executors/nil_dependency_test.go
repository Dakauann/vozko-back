package node_executors

import (
	"testing"

	"vozko/domain/workflow"
)

func nilDependencyCtx(id string, config map[string]interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Run:   &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "whatsapp"},
		Node:  &workflow.Node{ID: id, Config: config},
		Graph: &workflow.Graph{Edges: []workflow.Edge{{Source: id, Target: "ok", Label: "sucesso"}, {Source: id, Target: "bad", Label: "erro"}, {Source: id, Target: "yes", Label: "true"}, {Source: id, Target: "no", Label: "false"}}},
		State: &state,
	}
}

func TestAnExecutorWithoutItsStoreRefusesInsteadOfPanicking(t *testing.T) {
	cases := []struct {
		name     string
		executor workflow.NodeExecutor
		config   map[string]interface{}
	}{
		{"assign member without the member store", NewAssignMemberExecutor(nil, nil), map[string]interface{}{"member_id": "m-1"}},
		{"check label without the label store", NewCheckLabelExecutor(nil), map[string]interface{}{"label_id": "l-1"}},
		{"ai agent without the agent store", NewAIAgentExecutor(nil, nil, nil, nil, nil, nil), map[string]interface{}{"source": "agent", "agent_id": "a-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			res, err := tc.executor.Execute(nilDependencyCtx("n", tc.config))
			failed := err != nil || (res != nil && (res.Error != "" || res.Output["success"] == false))
			if !failed {
				t.Fatalf("a missing store must fail the node: res = %+v", res)
			}
			if err == nil && res != nil && res.Error != "" && res.ErrorKind == "" {
				t.Fatalf("the refusal must carry an error kind: %+v", res)
			}
		})
	}
}
