package workflow

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/ai"
)

const ConfigKeyReasoningLevel = "reasoning_level"

var ErrNodeReasoningLevelUnsupported = errors.New("workflow: the AI model does not offer the chosen reasoning level")

type AIModelChoice struct {
	Model string
	Level ai.ReasoningLevel
}

func (n *Node) InlineAIModelChoice() (AIModelChoice, bool) {
	if n.Type != NodeTypeActionAIAgent {
		return AIModelChoice{}, false
	}
	if source, _ := n.Config["source"].(string); strings.TrimSpace(source) != "prompt" {
		return AIModelChoice{}, false
	}
	model, _ := n.Config["model"].(string)
	level, _ := n.Config[ConfigKeyReasoningLevel].(string)
	return AIModelChoice{Model: strings.TrimSpace(model), Level: ai.ParseReasoningLevel(level)}, true
}

func (n *Node) chosenReasoning() (AIModelChoice, bool) {
	choice, ok := n.InlineAIModelChoice()
	return choice, ok && choice.Level != ""
}

func CheckNodeReasoning(n *Node, models []ai.ModelInfo) error {
	choice, ok := n.chosenReasoning()
	if !ok {
		return nil
	}
	if err := ai.CheckReasoning(models, choice.Model, choice.Level); err != nil {
		return fmt.Errorf("%w: node %q model %q level %q", ErrNodeReasoningLevelUnsupported, n.ID, choice.Model, choice.Level)
	}
	return nil
}

func CheckGraphReasoning(g *Graph, models []ai.ModelInfo) error {
	for i := range g.Nodes {
		if err := CheckNodeReasoning(&g.Nodes[i], models); err != nil {
			return err
		}
	}
	return nil
}

func GraphChoosesReasoning(g *Graph) bool {
	for i := range g.Nodes {
		if _, ok := g.Nodes[i].chosenReasoning(); ok {
			return true
		}
	}
	return false
}
