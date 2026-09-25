package node_executors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func channelGraph(labels ...string) *workflow.Graph {
	edges := make([]workflow.Edge, 0, len(labels))
	for i, label := range labels {
		edges = append(edges, workflow.Edge{
			Source: "branch",
			Target: "t" + label,
			Label:  label,
		})
		_ = i
	}
	return &workflow.Graph{
		Nodes: []workflow.Node{{ID: "branch", Type: workflow.NodeTypeConditionChannel}},
		Edges: edges,
	}
}

func runOnChannel(entryType string) *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID:          "run-1",
		WorkflowID:  "wf-1",
		WorkspaceID: "ws-1",
		EntryID:     "entry-1",
		EntryType:   entryType,
	}
}

func executeChannelBranch(t *testing.T, run *workflow.WorkflowRun, graph *workflow.Graph) *workflow.NodeResult {
	t.Helper()
	state := workflow.NewRunState()
	res, err := NewChannelBranchExecutor().Execute(&workflow.NodeContext{
		Run:   run,
		Node:  &graph.Nodes[0],
		Graph: graph,
		State: &state,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

func TestChannelBranch_RoutesToTheMatchingChannelEdge(t *testing.T) {
	graph := channelGraph("whatsapp", "instagram", "default")
	res := executeChannelBranch(t, runOnChannel("instagram"), graph)

	assert.Equal(t, "tinstagram", res.NextNodeID)
	assert.Equal(t, true, res.Output["matched"])
	assert.Equal(t, "instagram", res.Output["channel"])
}

func TestChannelBranch_SeparatesTheTwoWhatsAppTransports(t *testing.T) {
	graph := channelGraph("whatsapp", "unofficial_whatsapp", "default")

	official := executeChannelBranch(t, runOnChannel("whatsapp"), graph)
	assert.Equal(t, "twhatsapp", official.NextNodeID)

	unofficial := executeChannelBranch(t, runOnChannel("unofficial_whatsapp"), graph)
	assert.Equal(t, "tunofficial_whatsapp", unofficial.NextNodeID,
		"unofficial must not fall into the official handle")
}

func TestChannelBranch_FallsBackToDefault(t *testing.T) {
	graph := channelGraph("whatsapp", "default")
	res := executeChannelBranch(t, runOnChannel("telegram"), graph)

	assert.Equal(t, "tdefault", res.NextNodeID)
	assert.Equal(t, false, res.Output["matched"],
		"matched=false is what distinguishes 'handles telegram' from 'tolerates telegram'")
}

func TestChannelBranch_NoMatchAndNoDefaultEndsTheRun(t *testing.T) {
	graph := channelGraph("whatsapp")
	res := executeChannelBranch(t, runOnChannel("telegram"), graph)

	assert.Empty(t, res.NextNodeID)
	assert.Equal(t, false, res.Output["matched"])
}

func TestChannelBranch_DefaultOrderDoesNotShadowAnExactMatch(t *testing.T) {
	graph := channelGraph("default", "telegram")
	res := executeChannelBranch(t, runOnChannel("telegram"), graph)

	assert.Equal(t, "ttelegram", res.NextNodeID)
	assert.Equal(t, true, res.Output["matched"])
}

func TestChannelBranch_LabelMatchingIsCaseAndSpaceTolerant(t *testing.T) {
	graph := &workflow.Graph{
		Nodes: []workflow.Node{{ID: "branch", Type: workflow.NodeTypeConditionChannel}},
		Edges: []workflow.Edge{
			{Source: "branch", Target: "t1", Label: "  Instagram "},
			{Source: "branch", Target: "t2", Label: "default"},
		},
	}
	res := executeChannelBranch(t, runOnChannel("instagram"), graph)
	assert.Equal(t, "t1", res.NextNodeID, "a hand-edited label must still route")
}

func TestChannelBranch_EmptyChannelTakesTheDefault(t *testing.T) {
	graph := &workflow.Graph{
		Nodes: []workflow.Node{{ID: "branch", Type: workflow.NodeTypeConditionChannel}},
		Edges: []workflow.Edge{
			{Source: "branch", Target: "tblank", Label: ""},
			{Source: "branch", Target: "tdefault", Label: "default"},
		},
	}
	res := executeChannelBranch(t, runOnChannel(""), graph)

	assert.Equal(t, "tdefault", res.NextNodeID)
	assert.Equal(t, false, res.Output["matched"])
}

func TestChannelBranch_IgnoresAnOverwrittenChannelVariable(t *testing.T) {
	graph := channelGraph("whatsapp", "telegram", "default")
	run := runOnChannel("whatsapp")

	state := workflow.NewRunState()
	state.Set(workflow.VarChannel, "telegram")

	res, err := NewChannelBranchExecutor().Execute(&workflow.NodeContext{
		Run:   run,
		Node:  &graph.Nodes[0],
		Graph: graph,
		State: &state,
	})
	require.NoError(t, err)
	assert.Equal(t, "twhatsapp", res.NextNodeID,
		"routing follows the run, not a variable an author can shadow")
}

func TestChannelBranch_DeclaresAHandlePerChannelPlusDefault(t *testing.T) {
	def := NewChannelBranchExecutor().Definition()

	require.Len(t, def.Outputs, len(workflow.ChannelBranchOrder)+1)
	for i, entryType := range workflow.ChannelBranchOrder {
		assert.Equal(t, string(entryType), def.Outputs[i].ID)
		assert.NotEmpty(t, def.Outputs[i].Label, "every handle needs an operator-facing name")
	}
	last := def.Outputs[len(def.Outputs)-1]
	assert.Equal(t, workflow.ChannelBranchDefault, last.ID, "the default is always last")
}

func TestChannelBranch_HasNoConfiguration(t *testing.T) {
	def := NewChannelBranchExecutor().Definition()
	assert.Empty(t, def.ConfigSchema)
	assert.Empty(t, def.DefaultConfig)
}

func TestChannelBranch_IsAValidConditionNodeType(t *testing.T) {
	assert.True(t, workflow.NodeTypeConditionChannel.Valid(),
		"an unregistered type is rejected when a graph is saved")
	assert.True(t, workflow.NodeTypeConditionChannel.IsCondition(),
		"the editor renders branch handles only for condition nodes")
}

func TestChannelBranch_OffersOnlyReachableChannels(t *testing.T) {
	for _, entryType := range []shared.EntryType{
		shared.EntryTypeWhatsApp,
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
	} {
		assert.True(t, KnownChannelBranch(string(entryType)), string(entryType))
		assert.Contains(t, workflow.ChannelBranchOrder, entryType)
	}

	for _, entryType := range workflow.ChannelBranchOrder {
		assert.True(t, entryType.Valid(),
			"%s is not a messaging channel, so a run can never be on it", entryType)
	}

}

func TestChannelBranch_KnownHandleRejectsNonsense(t *testing.T) {
	assert.True(t, KnownChannelBranch(workflow.ChannelBranchDefault))
	assert.False(t, KnownChannelBranch("carrier_pigeon"))
}

func TestChannelOf_ReadsTheRunAndToleratesNil(t *testing.T) {
	assert.Equal(t, "telegram", workflow.ChannelOf(runOnChannel("telegram")))
	assert.Equal(t, "", workflow.ChannelOf(nil))
}
