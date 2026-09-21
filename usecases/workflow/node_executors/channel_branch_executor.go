package node_executors

import (
	"strings"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type channelBranchExecutor struct{}

func NewChannelBranchExecutor() workflow.NodeExecutor {
	return &channelBranchExecutor{}
}

func (e *channelBranchExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionChannel,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Canal",
		Description: "Divide o fluxo pelo canal em que a conversa está acontecendo.",
		Icon:        "Broadcast",
		Guidance: workflow.NodeGuidance{
			When: "Quando o mesmo fluxo atende vários canais e um trecho precisa mudar (ex.: template no WhatsApp oficial, texto simples no resto).",
			Examples: []string{
				"Sem config: as arestas são os canais. Ligue a saída \"whatsapp\" ao envio de template e \"default\" ao envio de texto.",
			},
		},
		Outputs:       workflow.ChannelBranchHandles(),
		DefaultConfig: map[string]interface{}{},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "channel", Description: "Canal da execução (whatsapp, instagram, telegram, ...)"},
			{Key: "matched", Description: "true quando havia uma aresta para esse canal; false quando caiu no padrão"},
		},
		ConfigSchema: []workflow.ConfigField{},
	}
}

func (e *channelBranchExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	channel := workflow.ChannelOf(ctx.Run)

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	targetID, matched := routeByChannel(edges, channel)

	return &workflow.NodeResult{
		NextNodeID: targetID,
		Output: map[string]interface{}{
			"channel": channel,
			"matched": matched,
		},
	}, nil
}

func routeByChannel(edges []workflow.Edge, channel string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(channel))

	var fallback string
	for _, edge := range edges {
		label := strings.ToLower(strings.TrimSpace(edge.Label))
		if label == normalized && normalized != "" {
			return edge.Target, true
		}
		if label == workflow.ChannelBranchDefault && fallback == "" {
			fallback = edge.Target
		}
	}
	return fallback, false
}

func KnownChannelBranch(handle string) bool {
	if handle == workflow.ChannelBranchDefault {
		return true
	}
	for _, entryType := range workflow.ChannelBranchOrder {
		if string(entryType) == handle {
			return true
		}
	}
	return shared.EntryType(handle).IsKnown()
}
