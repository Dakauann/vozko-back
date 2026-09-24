package tools_usecase

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	"vozko/domain/tools"
)

type outcomeCatalogueStub map[string]*conversation.OutcomeCapture

func (s outcomeCatalogueStub) OutcomeCaptureFor(_ context.Context, workspaceID string) (*conversation.OutcomeCapture, error) {
	return s[workspaceID], nil
}

func outcomeCatalogue(require bool) *conversation.OutcomeCapture {
	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	capture := &conversation.OutcomeCapture{
		Enabled:         true,
		EnabledAt:       &enabled,
		RequireOnFinish: require,
		Outcomes: []conversation.Outcome{
			{Code: "sale", Label: "Venda fechada", IsDurable: true, Position: 1},
			{Code: "no_answer", Label: "Sem resposta", Position: 2},
		},
	}
	capture.Normalize()
	return capture
}

func finishToolWith(status *finishStatusStub, catalogue outcomeCatalogueStub) *finishConversationTool {
	return NewFinishConversationToolUseCase(status, catalogue).(*finishConversationTool)
}

// The model can only pick an outcome it has been shown. The tool lists the
// workspace's catalogue, and a workspace that requires one makes it required,
// exactly as it does for a person finishing the conversation.
func TestFinishConversationToolListsTheWorkspaceOutcomes(t *testing.T) {
	ft := finishToolWith(&finishStatusStub{}, outcomeCatalogueStub{"ws-1": outcomeCatalogue(true)})

	def := ft.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"})

	param, ok := def.Parameters["outcome_code"]
	require.True(t, ok)
	require.Equal(t, []string{"sale", "no_answer"}, param.Enum)
	require.Contains(t, param.Description, "Venda fechada")
	require.Contains(t, param.Description, "Sem resposta")
	require.Contains(t, def.Required, "outcome_code")
}

func TestFinishConversationToolLeavesAnOptionalOutcomeOptional(t *testing.T) {
	ft := finishToolWith(&finishStatusStub{}, outcomeCatalogueStub{"ws-1": outcomeCatalogue(false)})

	def := ft.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"})

	require.Equal(t, []string{"sale", "no_answer"}, def.Parameters["outcome_code"].Enum)
	require.NotContains(t, def.Required, "outcome_code")
}

func TestFinishConversationToolHidesTheOutcomeWhenNotCollected(t *testing.T) {
	off := outcomeCatalogue(true)
	off.Enabled = false
	ft := finishToolWith(&finishStatusStub{}, outcomeCatalogueStub{"ws-off": off})

	for _, workspaceID := range []string{"ws-off", "ws-none", ""} {
		def := ft.DefinitionWithContext(tools.ToolContext{WorkspaceID: workspaceID})
		_, listed := def.Parameters["outcome_code"]
		require.False(t, listed, "workspace %q does not collect outcomes", workspaceID)
		require.NotContains(t, def.Required, "outcome_code")
	}
}

// A refusal teaches the model the valid choices so its next call can succeed,
// even when the definition it saw was not tailored to the workspace.
func TestFinishConversationToolExplainsARefusedOutcome(t *testing.T) {
	for _, refusal := range []error{conversation.ErrOutcomeRequired, conversation.ErrOutcomeUnknown} {
		status := &finishStatusStub{err: refusal}
		ft := finishToolWith(status, outcomeCatalogueStub{"ws-1": outcomeCatalogue(true)})

		out, err := ft.ExecuteWithConfig(context.Background(), map[string]interface{}{
			"__entry_id": "e-1", "__entry_type": "unofficial_whatsapp", "__workspace_id": "ws-1",
		}, map[string]interface{}{"outcome_code": "invented"})

		require.NoError(t, err)
		require.True(t, out.IsError)
		require.Contains(t, out.Result, "sale")
		require.Contains(t, out.Result, "Venda fechada")
		require.Contains(t, out.Result, "no_answer")
		require.Equal(t, "invented", status.lastOpts.OutcomeCode)
	}
}

func TestFinishConversationToolRecordsTheAgentThatClosed(t *testing.T) {
	status := &finishStatusStub{}
	ft := finishToolWith(status, nil)

	_, err := ft.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__entry_id": "e-1", "__entry_type": "telegram", "__agent_id": "agent-1",
	}, map[string]interface{}{"outcome_code": "sale"})

	require.NoError(t, err)
	require.Equal(t, actor.FormatAI("agent-1"), status.lastOpts.ActorID)
	require.Equal(t, "sale", status.lastOpts.OutcomeCode)
}
