package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/tools"
	ia_usecase "vozko/usecases/inbox_assignment"
)

type handOffStub struct {
	owner string
	err   error
	calls []string
	got   ia.RouletteHandOff
}

func (s *handOffStub) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	s.calls = append(s.calls, in.WorkspaceID+"|"+in.EntryID+"|"+in.EntryType)
	s.got = in
	return s.owner, s.err
}

func transferConfig() map[string]interface{} {
	return map[string]interface{}{
		"__workspace_id": "ws-1",
		"__entry_id":     "e-1",
		"__entry_type":   "telegram",
	}
}

func TestTransferToHuman_HandsOffTheCurrentConversation(t *testing.T) {
	// Pausing the AI is part of the hand-off itself (the assignment service),
	// shared with the workflow transfer nodes; the tool only names the conversation.
	handOff := &handOffStub{owner: "user-7"}
	tool := NewTransferToHumanToolUseCase(handOff)

	out, err := tool.ExecuteWithConfig(context.Background(), transferConfig(),
		map[string]interface{}{"reason": "cliente pediu um atendente"})

	require.NoError(t, err)
	require.False(t, out.IsError, out.Result)
	require.Equal(t, []string{"ws-1|e-1|telegram"}, handOff.calls)
	require.Contains(t, out.Result, "Avise o cliente")
}

func TestTransferToHuman_NobodyAvailableSaysItIsInTheTeamQueue(t *testing.T) {
	tool := NewTransferToHumanToolUseCase(&handOffStub{owner: ""})

	out, err := tool.ExecuteWithConfig(context.Background(), transferConfig(), nil)

	require.NoError(t, err)
	require.False(t, out.IsError)
	require.Contains(t, out.Result, "fila")
}

func TestTransferToHuman_HandOffFailureIsAnError(t *testing.T) {
	tool := NewTransferToHumanToolUseCase(&handOffStub{err: errors.New("db down")})

	out, err := tool.ExecuteWithConfig(context.Background(), transferConfig(), nil)

	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Contains(t, out.Result, "Continue o atendimento")
}

func TestTransferToHuman_APauseFailureIsNotReportedAsClean(t *testing.T) {
	// The person has it but the AI may keep answering: the model must still tell
	// the contact a person is taking over, and the log must not look clean.
	tool := NewTransferToHumanToolUseCase(&handOffStub{
		owner: "user-7",
		err:   fmt.Errorf("%w: e-1", ia_usecase.ErrAutomationStillActive),
	})

	out, err := tool.ExecuteWithConfig(context.Background(), transferConfig(), nil)

	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Contains(t, out.Result, "foi transferida")
}

func TestTransferToHuman_OutsideAConversationIsAnError(t *testing.T) {
	handOff := &handOffStub{}
	tool := NewTransferToHumanToolUseCase(handOff)

	for _, cfg := range []map[string]interface{}{
		nil,
		{"__entry_id": "e-1", "__entry_type": "telegram"},
		{"__workspace_id": "ws-1", "__entry_type": "telegram"},
		{"__workspace_id": "ws-1", "__entry_id": "e-1"},
	} {
		out, err := tool.ExecuteWithConfig(context.Background(), cfg, nil)
		require.NoError(t, err)
		require.True(t, out.IsError)
	}
	require.Empty(t, handOff.calls)
}

func TestTransferToHuman_NotBuiltWithoutAHandOff(t *testing.T) {
	require.Nil(t, NewTransferToHumanToolUseCase(nil))
}

func TestTransferToHuman_Definition(t *testing.T) {
	def := NewTransferToHumanToolUseCase(&handOffStub{}).Definition()

	require.Equal(t, TransferToHumanToolName, def.Name)
	require.Equal(t, []tools.ToolVisibility{tools.VisibilityMessaging}, def.Visibility,
		"a post-conversation pass must never move a conversation")
	require.Equal(t, tools.CategoryAgentUtility, def.Category)
}

func TestTransferToHuman_UsesTheDepartmentTheAdminConfigured(t *testing.T) {
	// The department comes from the agent's tool settings, set by an admin;
	// the model never names one, so it cannot invent "Financeiro".
	handOff := &handOffStub{owner: "user-7"}
	tool := NewTransferToHumanToolUseCase(handOff)
	cfg := transferConfig()
	cfg["department_id"] = "dept-sales"

	_, err := tool.ExecuteWithConfig(context.Background(), cfg,
		map[string]interface{}{"department_id": "dept-made-up"})

	require.NoError(t, err)
	require.Equal(t, "dept-sales", handOff.got.DepartmentID)
}

func TestTransferToHuman_WithoutAConfiguredDepartmentUsesTheConversations(t *testing.T) {
	handOff := &handOffStub{owner: "user-7"}

	_, err := NewTransferToHumanToolUseCase(handOff).ExecuteWithConfig(context.Background(), transferConfig(), nil)

	require.NoError(t, err)
	require.Equal(t, "", handOff.got.DepartmentID)
	require.Equal(t, "", handOff.got.ByActorID, "credited to the agent holding the conversation")
}

func TestTransferToHuman_ADepartmentOutsideTheWorkspaceIsAnError(t *testing.T) {
	tool := NewTransferToHumanToolUseCase(&handOffStub{err: fmt.Errorf("wrap: %w", ia.ErrDepartmentOutOfScope)})

	out, err := tool.ExecuteWithConfig(context.Background(), transferConfig(), nil)

	require.NoError(t, err)
	require.True(t, out.IsError)
}

func TestTransferToHuman_OffersADepartmentSettingButNoModelParameter(t *testing.T) {
	def := NewTransferToHumanToolUseCase(&handOffStub{}).Definition()

	setting, ok := def.ConfigSchema["department_id"]
	require.True(t, ok, "admins pick the department in the agent's tool settings")
	require.False(t, setting.Required)
	require.Equal(t, "departments", setting.OptionsSource)
	_, exposed := def.Parameters["department_id"]
	require.False(t, exposed, "the model must not choose a department")
	require.NoError(t, def.ValidateConfig(map[string]interface{}{"department_id": "dept-sales"}))
	require.NoError(t, def.ValidateConfig(map[string]interface{}{}))
}
