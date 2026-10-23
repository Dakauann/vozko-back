package workflow

import "vozko/domain/choicestats"

const StateKeyChoicePrompt = "_choice_prompt_key"

func ChoicePromptKey(run *WorkflowRun) string {
	return choicestats.WorkflowPromptKey(run.ID, run.NodeVisit())
}

func ChoicePromptSource(run *WorkflowRun, nodeID, templateID string) choicestats.Source {
	version := 0
	if run.WorkflowVersion != nil {
		version = *run.WorkflowVersion
	}
	return choicestats.Source{Kind: choicestats.SourceWorkflow, ID: run.WorkflowID, NodeID: nodeID, Version: version, TemplateID: templateID}
}

func InteractiveOptionIDs(config map[string]interface{}) []string {
	handles := InteractiveOptionHandles(config)
	ids := make([]string, 0, len(handles))
	for _, h := range handles {
		ids = append(ids, h.ID)
	}
	return ids
}

func ChoiceCanvas(g *Graph) []choicestats.CanvasNode {
	var out []choicestats.CanvasNode
	for i := range g.Nodes {
		node := &g.Nodes[i]
		switch {
		case node.Type.IsInteractivePrompt():
			ids := InteractiveOptionIDs(node.Config)
			options := make([]choicestats.OfferedOption, 0, len(ids))
			for _, id := range ids {
				options = append(options, choicestats.OfferedOption{ID: id})
			}
			out = append(out, choicestats.CanvasNode{NodeID: node.ID, Options: options})
		case node.Type.Canonical() == NodeTypeActionSendTemplate:
			out = append(out, choicestats.CanvasNode{NodeID: node.ID})
		}
	}
	return out
}
