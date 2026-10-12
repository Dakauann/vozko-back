package workflow

import (
	"errors"
	"testing"

	"vozko/domain/ai"
)

func aiNode(config map[string]interface{}) *Node {
	return &Node{ID: "n1", Type: NodeTypeActionAIAgent, Config: config}
}

func reasoningCatalog() []ai.ModelInfo {
	return []ai.ModelInfo{ai.DescribeModel(ai.ModelFacts{
		ID:         "lab/thinker",
		Parameters: []string{"tools", "reasoning"},
		Reasoning:  &ai.ReasoningFacts{Efforts: []string{"low", "high"}, Default: "low"},
	})}
}

func TestInlineAIModelChoiceReadsTheModelAndLevel(t *testing.T) {
	choice, ok := aiNode(map[string]interface{}{"source": "prompt", "model": " lab/thinker ", ConfigKeyReasoningLevel: " high "}).InlineAIModelChoice()
	if !ok || choice.Model != "lab/thinker" || choice.Level != ai.ReasoningHigh {
		t.Fatalf("choice = %+v, %v", choice, ok)
	}
	for _, n := range []*Node{
		aiNode(map[string]interface{}{"model": "lab/thinker", ConfigKeyReasoningLevel: "high"}),
		aiNode(map[string]interface{}{"source": "agent", "model": "lab/thinker", ConfigKeyReasoningLevel: "high"}),
		{ID: "x", Type: NodeTypeActionSendText, Config: map[string]interface{}{"source": "prompt", ConfigKeyReasoningLevel: "high"}},
	} {
		if _, ok := n.InlineAIModelChoice(); ok {
			t.Fatalf("node %+v must not carry an inline model choice", n)
		}
	}
}

func TestCheckNodeReasoningUsesTheModelOffer(t *testing.T) {
	cases := []struct {
		name   string
		node   *Node
		models []ai.ModelInfo
		err    error
	}{
		{name: "an offered level passes", node: aiNode(map[string]interface{}{"source": "prompt", "model": "lab/thinker", ConfigKeyReasoningLevel: "high"}), models: reasoningCatalog()},
		{name: "no level passes without a catalog", node: aiNode(map[string]interface{}{"source": "prompt", "model": "lab/thinker"})},
		{name: "agent mode ignores the level", node: aiNode(map[string]interface{}{"source": "agent", ConfigKeyReasoningLevel: "max"})},
		{name: "a level outside the offer is refused", node: aiNode(map[string]interface{}{"source": "prompt", "model": "lab/thinker", ConfigKeyReasoningLevel: "max"}), models: reasoningCatalog(), err: ErrNodeReasoningLevelUnsupported},
		{name: "an unknown catalog refuses", node: aiNode(map[string]interface{}{"source": "prompt", "model": "lab/thinker", ConfigKeyReasoningLevel: "high"}), err: ErrNodeReasoningLevelUnsupported},
		{name: "a templated model cannot be checked and is refused", node: aiNode(map[string]interface{}{"source": "prompt", "model": "{{var.model}}", ConfigKeyReasoningLevel: "high"}), models: reasoningCatalog(), err: ErrNodeReasoningLevelUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckNodeReasoning(tc.node, tc.models); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestCheckGraphReasoningStopsAtTheFirstRefusedNode(t *testing.T) {
	g := &Graph{Nodes: []Node{
		*aiNode(map[string]interface{}{"source": "prompt", "model": "lab/thinker", ConfigKeyReasoningLevel: "low"}),
		{ID: "n2", Type: NodeTypeActionAIAgent, Config: map[string]interface{}{"source": "prompt", "model": "lab/thinker", ConfigKeyReasoningLevel: "max"}},
	}}
	if err := CheckGraphReasoning(g, reasoningCatalog()); !errors.Is(err, ErrNodeReasoningLevelUnsupported) {
		t.Fatalf("err = %v, want refusal", err)
	}
	if !GraphChoosesReasoning(g) || GraphChoosesReasoning(&Graph{Nodes: []Node{*aiNode(map[string]interface{}{"source": "prompt", "model": "lab/thinker"})}}) {
		t.Fatal("GraphChoosesReasoning must be true only when a node sets a level")
	}
}
