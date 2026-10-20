package workflow_usecase

import (
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestTheBuilderPromptNeverCarriesPinnedOutputs(t *testing.T) {
	g := workflow.Graph{Nodes: []workflow.Node{
		{ID: "login", Type: workflow.NodeTypeActionHTTPRequest, Pinned: &workflow.NodePin{
			Output: map[string]interface{}{"token": "production-output-that-must-stay-out"}, Source: workflow.PinSourceOfRun("run-1"), PinnedAt: productionPinAt,
		}},
	}}

	prompt := serializeGraph(&g)
	if strings.Contains(prompt, "production-output-that-must-stay-out") || strings.Contains(prompt, "pinned") {
		t.Fatalf("the model would read a pinned production output on every iteration:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"login"`) {
		t.Fatalf("the node itself must stay in the prompt:\n%s", prompt)
	}
	if g.Nodes[0].Pinned == nil {
		t.Fatal("serializing for the prompt must not drop the pin from the draft")
	}
}
