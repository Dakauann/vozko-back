package node_executors

import (
	"strings"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// channelBranchExecutor forks a run on the channel it is executing on.
//
// It exists because "same flow, different channel" is the common shape and the
// alternatives are both bad. Duplicating a whole workflow per channel means
// every later edit has to be made N times and drift is a matter of when.
// Branching with a generic condition node on {{channel}} works, but puts the
// channel's spelling in a free-text field where a typo silently routes every
// run down the false edge — and the author only finds out from a customer.
//
// This node names the channels as handles, so the graph shows which channels a
// flow actually handles, and an unhandled one has a visible default edge rather
// than a dead end.
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
		// No config: the channel is a fact about the run, not a setting. A
		// config field here would be a field whose only correct value is the
		// one the runtime already knows.
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

// routeByChannel picks the edge for this channel, then the default edge.
//
// Returns matched=false when the run fell through to the default, so a report
// can tell "this flow handles Telegram" from "this flow tolerates Telegram".
// An empty target is a legitimate outcome — a channel branch with no default
// and no matching edge ends the run, exactly as any other node with no
// outgoing edge does.
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

// KnownChannelBranch reports whether a handle id names a channel this build
// knows. Used by the graph linter to flag an edge labelled with a channel that
// no longer exists after a rename.
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
