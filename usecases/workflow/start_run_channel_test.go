package workflow_usecase

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type startRunHarness struct {
	uc       workflow.StartRunUseCase
	runs     *MockWorkflowRunRepository
	launcher *fakeLauncher
}

func (h startRunHarness) created(t *testing.T) *workflow.WorkflowRun {
	t.Helper()
	require.Len(t, h.launcher.launched, 1, "the run must be launched on the engine, not left for the stuck-run sweep")
	stored, err := h.runs.FindByID(h.launcher.launched[0].ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	return stored
}

func startRunFixture(t *testing.T, visibility conversation.Visibility) startRunHarness {
	t.Helper()
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, waitingGraph("wait", "end"))
	runs := NewMockWorkflowRunRepository()
	launcher := &fakeLauncher{}
	starter, err := NewConversationRunStarter(repo, runs, launcher, &fakeSharedState{allow: true})
	require.NoError(t, err)
	return startRunHarness{uc: NewStartRunUseCase(starter, runs, visibility), runs: runs, launcher: launcher}
}

func manualStart(entryType string) workflow.StartRunInput {
	return workflow.StartRunInput{
		WorkflowID:  "wf-1",
		WorkspaceID: "ws1",
		EntryID:     "entry-1",
		EntryType:   entryType,
		StartedBy:   workflow.StartedByManual("user-1"),
		Viewer:      shared.Person{UserID: "user-1"},
	}
}

func TestStartRun_SeedsTheChannelVariableFromTheEntryType(t *testing.T) {
	h := startRunFixture(t, seesEverything{})

	_, err := h.uc.Execute(manualStart("instagram"))
	require.NoError(t, err)

	assert.Equal(t, "instagram", h.created(t).State.GetString(workflow.VarChannel))
}

func TestStartRun_CallerVariablesCannotSpoofTheChannel(t *testing.T) {
	h := startRunFixture(t, seesEverything{})
	in := manualStart("whatsapp")
	in.Variables = map[string]interface{}{workflow.VarChannel: "telegram", "nome": "Ana"}

	_, err := h.uc.Execute(in)
	require.NoError(t, err)

	created := h.created(t)
	assert.Equal(t, "whatsapp", created.State.GetString(workflow.VarChannel),
		"the run's own entry type wins over anything the caller passed")
	assert.Equal(t, "Ana", created.State.GetString("nome"),
		"unrelated caller variables are untouched")
}

func TestStartRun_SeededVariableAgreesWithChannelOf(t *testing.T) {
	h := startRunFixture(t, seesEverything{})

	_, err := h.uc.Execute(manualStart(string(shared.EntryTypeUnofficialWhatsApp)))
	require.NoError(t, err)

	created := h.created(t)
	assert.Equal(t, workflow.ChannelOf(created), created.State.GetString(workflow.VarChannel))
}

func TestStartRun_PinsTheRunToThePublishedVersionAndNamesTheStarter(t *testing.T) {
	h := startRunFixture(t, seesEverything{})

	run, err := h.uc.Execute(manualStart("whatsapp"))
	require.NoError(t, err)
	require.NotNil(t, run)

	created := h.created(t)
	assert.Equal(t, created.ID, run.ID)
	require.NotNil(t, created.WorkflowVersion, "a manual run must record the version it started on")
	assert.Equal(t, 1, *created.WorkflowVersion)
	assert.Equal(t, workflow.StartedByManual("user-1"), created.StartedBy)
}

func TestStartRun_AWorkflowOfAnotherWorkspaceIsNotFound(t *testing.T) {
	h := startRunFixture(t, seesEverything{})
	in := manualStart("whatsapp")
	in.WorkspaceID = "ws-other"

	_, err := h.uc.Execute(in)
	require.ErrorIs(t, err, workflow.ErrWorkflowNotFound)
	assert.Empty(t, h.launcher.launched)
}

func TestStartRun_RefusesAConversationTheRequesterCannotSee(t *testing.T) {
	cases := []struct {
		name       string
		visibility conversation.Visibility
	}{
		{"hidden by the inbox rule", inboxRule{conversations: map[string]inboxConversation{}}},
		{"visibility unreadable", inboxRule{err: errors.New("db down")}},
		{"no visibility wired", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := startRunFixture(t, tc.visibility)

			_, err := h.uc.Execute(manualStart("whatsapp"))
			require.ErrorIs(t, err, conversation.ErrNotVisible)
			assert.Empty(t, h.launcher.launched)
		})
	}
}

func TestStartRun_AnExistingRunOnTheConversationIsAConflict(t *testing.T) {
	h := startRunFixture(t, seesEverything{})
	_, err := h.uc.Execute(manualStart("whatsapp"))
	require.NoError(t, err)

	_, err = h.uc.Execute(manualStart("whatsapp"))
	require.ErrorIs(t, err, workflow.ErrRunAlreadyExists)
	assert.Len(t, h.launcher.launched, 1)
}

func TestStartRun_BeginsAtTheMessageTriggerAsTheCustomersMessage(t *testing.T) {
	h := startRunFixture(t, seesEverything{})
	in := manualStart("whatsapp")
	in.Message = "Quero saber das turmas"

	_, err := h.uc.Execute(in)
	require.NoError(t, err)

	created := h.created(t)
	assert.Equal(t, "t", created.TriggerNodeID)
	want := workflow.MessageStartRunData(workflow.MessageStart{Text: "Quero saber das turmas", Channel: "whatsapp", WorkspaceID: "ws1"})
	for key, value := range want {
		got, _ := created.State.Get(key)
		assert.Equal(t, value, got, key)
	}
}

func TestStartRun_WithoutAMessageItStartsFromAnEmptyMessage(t *testing.T) {
	h := startRunFixture(t, seesEverything{})

	_, err := h.uc.Execute(manualStart("whatsapp"))
	require.NoError(t, err)

	created := h.created(t)
	assert.Equal(t, "", created.State.GetString(workflow.DataKeyMessage))
	assert.Equal(t, workflow.InboundTextMessage, created.State.GetString(workflow.DataKeyMessageType))
}

func TestStartRun_RefusesAWorkflowWithoutAMessageTrigger(t *testing.T) {
	repo := NewMockWorkflowRepository()
	graph := waitingGraph("wait", "end")
	graph.Nodes[0].Type = workflow.NodeTypeTriggerWebhook
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, graph)
	runs := NewMockWorkflowRunRepository()
	launcher := &fakeLauncher{}
	starter, err := NewConversationRunStarter(repo, runs, launcher, &fakeSharedState{allow: true})
	require.NoError(t, err)

	_, err = NewStartRunUseCase(starter, runs, seesEverything{}).Execute(manualStart("whatsapp"))
	require.ErrorIs(t, err, workflow.ErrWorkflowNeedsMessageTrigger)
	assert.Empty(t, launcher.launched)
}
