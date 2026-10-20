package workflow_usecase

import (
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

func interactiveBuilderState() *builderState {
	return &builderState{
		graph:          &workflow.Graph{},
		clientIDs:      map[string]string{},
		inspectedSpecs: map[string]bool{},
		fullCatalog:    []workflow.NodeDefinition{node_executors.NewInteractivePromptExecutor(node_executors.SenderDeps{}).Definition()},
		wfType:         workflow.WorkflowTypeMessages,
	}
}

func TestTheBuilderStoresOptionsInTheCanonicalShapeWhateverKeysTheModelWrote(t *testing.T) {
	uc := &aiBuilderUC{}
	st := interactiveBuilderState()

	_, err := uc.applyAddNode(st, ai.ToolCall{Name: toolAddNode, Arguments: map[string]interface{}{
		"type":        string(workflow.NodeTypeActionSendInteractive),
		"config_json": `{"interactive_type":"list","body":"Escolha","sections":"[{\"Title\":\"Aulas\",\"Rows\":[{\"ID\":\"1\",\"Title\":\"Um\"},{\"ID\":\"2\",\"Title\":\"Dois\"}]}]"}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	node := st.graph.Nodes[0]
	if got, want := node.Config["sections"], `[{"title":"Aulas","rows":[{"id":"1","title":"Um","description":""},{"id":"2","title":"Dois","description":""}]}]`; got != want {
		t.Fatalf("sections after add_node = %v\nwant %s", got, want)
	}

	_, err = uc.applyUpdateNode(st, ai.ToolCall{Name: toolUpdateNode, Arguments: map[string]interface{}{
		"id":          node.ID,
		"config_json": `{"buttons":"[{\"type\":\"reply\",\"id\":\"sim\",\"title\":\"Sim\"}]"}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := st.graph.Nodes[0].Config["buttons"], `[{"Type":"reply","ID":"sim","Title":"Sim","CopyCode":""}]`; got != want {
		t.Fatalf("buttons after update_node = %v\nwant %s", got, want)
	}
}

func TestTheInteractiveNodeSpecSpellsOutTheExactOptionKeys(t *testing.T) {
	spec := (&aiBuilderUC{}).handleGetNodeSpec(interactiveBuilderState(), ai.ToolCall{Name: toolGetNodeSpec, Arguments: map[string]interface{}{"node_type": string(workflow.NodeTypeActionSendInteractive)}})

	for _, shape := range []string{
		`{"title":"...","rows":[{"id":"...","title":"...","description":"..."}]}`,
		`{"Type":"reply","ID":"...","Title":"...","CopyCode":""}`,
	} {
		if !strings.Contains(spec, shape) {
			t.Fatalf("the spec must show the exact shape %s:\n%s", shape, spec)
		}
	}
}
