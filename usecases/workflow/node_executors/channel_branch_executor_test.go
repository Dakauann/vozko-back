package node_executors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// channelGraph builds a graph whose single branch node has one outgoing edge
// per label given, in order.
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

// ── routing ─────────────────────────────────────────────────────────────────

func TestChannelBranch_RoutesToTheMatchingChannelEdge(t *testing.T) {
	graph := channelGraph("whatsapp", "instagram", "default")
	res := executeChannelBranch(t, runOnChannel("instagram"), graph)

	assert.Equal(t, "tinstagram", res.NextNodeID)
	assert.Equal(t, true, res.Output["matched"])
	assert.Equal(t, "instagram", res.Output["channel"])
}

// The two WhatsApp transports are separate handles. They differ in what they can
// actually send — no template, no messaging window on the unofficial one — so
// collapsing them would send a template down a path that cannot deliver it.
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

// A branch with no default and no matching edge ends the run, exactly as any
// other node with no outgoing edge does. Inventing a target here would send the
// conversation somewhere the author never drew.
func TestChannelBranch_NoMatchAndNoDefaultEndsTheRun(t *testing.T) {
	graph := channelGraph("whatsapp")
	res := executeChannelBranch(t, runOnChannel("telegram"), graph)

	assert.Empty(t, res.NextNodeID)
	assert.Equal(t, false, res.Output["matched"])
}

func TestChannelBranch_DefaultOrderDoesNotShadowAnExactMatch(t *testing.T) {
	// Default drawn FIRST: the exact channel must still win.
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

// An empty channel must not match an empty label — that would route every
// channel-less run into whichever edge happened to have a blank label.
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

// The node reads the RUN, not the state variable. An author who overwrites
// {{channel}} with set_variable changes what interpolates, not where the
// conversation goes.
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

// ── definition ──────────────────────────────────────────────────────────────

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

// The node has no settings: the channel is a fact about the run, so a config
// field here would be one whose only correct value the runtime already knows.
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

// Every channel the platform can actually run a workflow on must have a handle,
// and NOTHING else may. A handle for a channel a run can never be on is worse
// than a missing one: the author draws an edge from it and gets a path that
// silently never fires.
//
// EntryType.Valid() IS the messaging-channel test, which is why voice fails it.
func TestChannelBranch_OffersOnlyReachableChannels(t *testing.T) {
	for _, entryType := range []shared.EntryType{
		shared.EntryTypeWhatsApp,
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
		shared.EntryTypeSupport,
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
