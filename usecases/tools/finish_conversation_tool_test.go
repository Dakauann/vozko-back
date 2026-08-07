package tools_usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/tools"
)

type finishStatusStub struct {
	lastEntryID   string
	lastEntryType string
	lastOpts      conversation.FinishOptions
	err           error
	calls         int
}

func (s *finishStatusStub) GetConversationStatus(string, string) conversation.ConversationStatus {
	return conversation.ConversationStatusOngoing
}
func (s *finishStatusStub) SetConversationStatus(string, string, conversation.ConversationStatus) error {
	return nil
}
func (s *finishStatusStub) Finish(entryID, entryType string, opts conversation.FinishOptions) error {
	s.calls++
	s.lastEntryID = entryID
	s.lastEntryType = entryType
	s.lastOpts = opts
	return s.err
}
func (s *finishStatusStub) TransitionOnMessage(string, string, conversation.MessageType, conversation.MessageHistoryDirection) error {
	return nil
}
func (s *finishStatusStub) GetStatusCounts(string, string, string) (map[string]int64, error) {
	return nil, nil
}

func TestFinishConversationTool_Success(t *testing.T) {
	st := &finishStatusStub{}
	ft := NewFinishConversationToolUseCase(st, nil).(*finishConversationTool)
	out, err := ft.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__entry_id":   "e-9",
		"__entry_type": "whatsapp",
	}, map[string]interface{}{"reason": "cliente agradeceu"})
	require.NoError(t, err)
	require.False(t, out.IsError)
	require.Equal(t, 1, st.calls)
	require.Equal(t, "e-9", st.lastEntryID)
	require.Equal(t, "whatsapp", st.lastEntryType)
	require.Equal(t, conversation.CloseSourceAI, st.lastOpts.Source)
	require.Equal(t, conversation.CloseReasonAIResolved, st.lastOpts.Reason)
	require.Contains(t, out.Result, "cliente agradeceu")
}

func TestFinishConversationTool_MissingConfig(t *testing.T) {
	st := &finishStatusStub{}
	ft := NewFinishConversationToolUseCase(st, nil).(*finishConversationTool)
	out, err := ft.ExecuteWithConfig(context.Background(), nil, nil)
	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Equal(t, 0, st.calls)
}

func TestFinishConversationTool_NilStatus(t *testing.T) {
	require.Nil(t, NewFinishConversationToolUseCase(nil, nil))
}

func TestFinishConversationTool_Definition(t *testing.T) {
	ft := NewFinishConversationToolUseCase(&finishStatusStub{}, nil).(*finishConversationTool)
	def := ft.Definition()
	require.Equal(t, FinishConversationToolName, def.Name)
	require.Contains(t, def.Visibility, tools.VisibilityMessaging)
	require.Equal(t, tools.CategoryAgentUtility, def.Category)
}

func TestFinishConversationTool_UnsupportedType(t *testing.T) {
	st := &finishStatusStub{}
	ft := NewFinishConversationToolUseCase(st, nil).(*finishConversationTool)
	out, err := ft.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__entry_id":   "e-1",
		"__entry_type": "lead",
	}, nil)
	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Equal(t, 0, st.calls)
}

func TestFinishConversationTool_ServiceError(t *testing.T) {
	st := &finishStatusStub{err: errors.New("boom")}
	ft := NewFinishConversationToolUseCase(st, nil).(*finishConversationTool)
	out, err := ft.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__entry_id":   "e-1",
		"__entry_type": "whatsapp",
	}, nil)
	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Equal(t, 1, st.calls)
}

func TestFinishConversationTool_VoiceEntry(t *testing.T) {
	st := &finishStatusStub{}
	ft := NewFinishConversationToolUseCase(st, nil).(*finishConversationTool)
	out, err := ft.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__entry_id":   "v-2",
		"__entry_type": "voice",
	}, map[string]interface{}{})
	require.NoError(t, err)
	require.False(t, out.IsError)
	require.Equal(t, "v-2", st.lastEntryID)
	require.Equal(t, "voice", st.lastEntryType)
	require.Equal(t, conversation.CloseSourceAI, st.lastOpts.Source)
	require.Equal(t, conversation.CloseReasonAIResolved, st.lastOpts.Reason)
}
