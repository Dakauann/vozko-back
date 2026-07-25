package node_executors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

type finishStatusMock struct {
	calls         int
	lastEntryID   string
	lastEntryType string
	lastOpts      conversation.FinishOptions
	err           error
}

func (m *finishStatusMock) GetConversationStatus(string, string) conversation.ConversationStatus {
	return conversation.ConversationStatusOngoing
}
func (m *finishStatusMock) SetConversationStatus(string, string, conversation.ConversationStatus) error {
	return nil
}
func (m *finishStatusMock) Finish(entryID, entryType string, opts conversation.FinishOptions) error {
	m.calls++
	m.lastEntryID = entryID
	m.lastEntryType = entryType
	m.lastOpts = opts
	return m.err
}
func (m *finishStatusMock) TransitionOnMessage(string, string, conversation.MessageType) error {
	return nil
}
func (m *finishStatusMock) GetStatusCounts(string, string, string) (map[string]int64, error) {
	return nil, nil
}

func finishConvCtx(entryID, entryType string, config map[string]interface{}, edges []workflow.Edge) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Node: &workflow.Node{
			ID:     "n1",
			Type:   workflow.NodeTypeActionFinishConversation,
			Config: config,
		},
		Graph: &workflow.Graph{Edges: edges},
		Run: &workflow.WorkflowRun{
			ID:          "run1",
			WorkspaceID: "ws1",
			EntryID:     entryID,
			EntryType:   entryType,
		},
		State: &state,
	}
}

func finishConvEdges() []workflow.Edge {
	return []workflow.Edge{
		{Source: "n1", Target: "ok", Label: "sucesso"},
		{Source: "n1", Target: "fail", Label: "erro"},
	}
}

func TestFinishConversationExecutor_Definition(t *testing.T) {
	exec := NewFinishConversationExecutor(&finishStatusMock{})
	def := exec.Definition()
	require.Equal(t, workflow.NodeTypeActionFinishConversation, def.Type)
	require.Equal(t, workflow.NodeCategoryAction, def.Category)
	require.Contains(t, def.Scopes, workflow.NodeScopeShared)
	require.NotEmpty(t, def.Guidance.When)
	require.True(t, def.Type.Valid())
}

func TestFinishConversationExecutor_Success(t *testing.T) {
	st := &finishStatusMock{}
	exec := NewFinishConversationExecutor(st)
	ctx := finishConvCtx("e-1", "whatsapp", map[string]interface{}{"note": "done"}, finishConvEdges())

	result, err := exec.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", result.NextNodeID)
	require.Equal(t, true, result.Output["success"])
	require.Equal(t, "e-1", result.Output["entry_id"])
	require.Equal(t, "whatsapp", result.Output["entry_type"])
	require.Equal(t, "system", result.Output["close_source"])
	require.Equal(t, "workflow", result.Output["close_reason"])
	require.Equal(t, "done", result.Output["note"])
	require.Equal(t, 1, st.calls)
	require.Equal(t, conversation.CloseSourceSystem, st.lastOpts.Source)
	require.Equal(t, conversation.CloseReasonWorkflow, st.lastOpts.Reason)
}

func TestFinishConversationExecutor_VoiceEntry(t *testing.T) {
	st := &finishStatusMock{}
	exec := NewFinishConversationExecutor(st)
	ctx := finishConvCtx("v-9", "voice", nil, finishConvEdges())

	result, err := exec.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", result.NextNodeID)
	require.Equal(t, "v-9", st.lastEntryID)
	require.Equal(t, "voice", st.lastEntryType)
}

func TestFinishConversationExecutor_MissingEntry(t *testing.T) {
	st := &finishStatusMock{}
	exec := NewFinishConversationExecutor(st)
	ctx := finishConvCtx("", "whatsapp", nil, finishConvEdges())

	result, err := exec.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, false, result.Output["success"])
	require.Equal(t, 0, st.calls)
}

func TestFinishConversationExecutor_UnsupportedType(t *testing.T) {
	st := &finishStatusMock{}
	exec := NewFinishConversationExecutor(st)
	ctx := finishConvCtx("e-1", "lead", nil, finishConvEdges())

	result, err := exec.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, 0, st.calls)
}

func TestFinishConversationExecutor_ServiceError(t *testing.T) {
	st := &finishStatusMock{err: errors.New("db down")}
	exec := NewFinishConversationExecutor(st)
	ctx := finishConvCtx("e-1", "whatsapp", nil, finishConvEdges())

	result, err := exec.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, false, result.Output["success"])
	require.Contains(t, result.Output["error"], "db down")
	require.Equal(t, 1, st.calls)
}

func TestFinishConversationExecutor_NilStatusService(t *testing.T) {
	exec := NewFinishConversationExecutor(nil)
	ctx := finishConvCtx("e-1", "whatsapp", nil, finishConvEdges())

	result, err := exec.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, false, result.Output["success"])
}
