package workflow_usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// stubWorkflowRepoForStart returns one active workflow with a trigger node.
type stubWorkflowRepoForStart struct{ wf *workflow.Workflow }

func (s *stubWorkflowRepoForStart) Create(*workflow.Workflow) error { return nil }
func (s *stubWorkflowRepoForStart) Update(*workflow.Workflow) error { return nil }
func (s *stubWorkflowRepoForStart) Delete(string) error             { return nil }
func (s *stubWorkflowRepoForStart) FindByID(string) (*workflow.Workflow, error) {
	return s.wf, nil
}
func (s *stubWorkflowRepoForStart) FindByWorkspaceID(string) ([]*workflow.Workflow, error) {
	return nil, nil
}
func (s *stubWorkflowRepoForStart) List(workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	return nil, nil
}
func (s *stubWorkflowRepoForStart) FindActiveByTrigger(string, workflow.TriggerType) ([]*workflow.Workflow, error) {
	return nil, nil
}

// captureRunRepo keeps whatever run the usecase created.
type captureRunRepo struct{ created *workflow.WorkflowRun }

func (c *captureRunRepo) Create(run *workflow.WorkflowRun) error { c.created = run; return nil }
func (c *captureRunRepo) Update(*workflow.WorkflowRun) error     { return nil }
func (c *captureRunRepo) FindByID(string) (*workflow.WorkflowRun, error) {
	return nil, nil
}
func (c *captureRunRepo) FindActiveByEntry(string, string) (*workflow.WorkflowRun, error) {
	return nil, nil
}
func (c *captureRunRepo) FindActiveByEntryAndTrigger(string, string, string) (*workflow.WorkflowRun, error) {
	return nil, nil
}
func (c *captureRunRepo) FindWaitingReplyByEntry(string) (*workflow.WorkflowRun, error) {
	return nil, nil
}
func (c *captureRunRepo) List(workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	return nil, nil
}
func (c *captureRunRepo) FindWakeableRuns(int64, int) ([]*workflow.WorkflowRun, error) {
	return nil, nil
}
func (c *captureRunRepo) FindStuckRuns(int64) ([]*workflow.WorkflowRun, error) { return nil, nil }
func (c *captureRunRepo) CancelByWorkflow(string) (int64, error)               { return 0, nil }
func (c *captureRunRepo) CountByWorkflow(string) (int64, error)                { return 0, nil }
func (c *captureRunRepo) CountActiveByWorkspace(string) (int64, error)         { return 0, nil }

func startRunFixture(t *testing.T) (workflow.StartRunUseCase, *captureRunRepo) {
	t.Helper()
	wf := &workflow.Workflow{
		ID:          "wf-1",
		WorkspaceID: "ws-1",
		Status:      workflow.WorkflowStatusActive,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "trigger-1", Type: workflow.NodeTypeTriggerMessageReceived},
			},
		},
	}
	runs := &captureRunRepo{}
	return NewStartRunUseCase(&stubWorkflowRepoForStart{wf: wf}, runs), runs
}

// The channel is available to every workflow as {{channel}} without the author
// declaring anything.
func TestStartRun_SeedsTheChannelVariableFromTheEntryType(t *testing.T) {
	uc, runs := startRunFixture(t)

	_, err := uc.Execute(workflow.StartRunInput{
		WorkflowID:  "wf-1",
		WorkspaceID: "ws-1",
		EntryID:     "entry-1",
		EntryType:   "instagram",
	})
	require.NoError(t, err)
	require.NotNil(t, runs.created)

	assert.Equal(t, "instagram", runs.created.State.GetString(workflow.VarChannel))
}

// A caller cannot start a run that lies about its channel. Seeding after the
// caller's variables is what makes {{channel}} trustworthy — otherwise every
// interpolation of it would be wrong and the failure would surface as a
// customer receiving the wrong kind of message.
func TestStartRun_CallerVariablesCannotSpoofTheChannel(t *testing.T) {
	uc, runs := startRunFixture(t)

	_, err := uc.Execute(workflow.StartRunInput{
		WorkflowID:  "wf-1",
		WorkspaceID: "ws-1",
		EntryID:     "entry-1",
		EntryType:   "whatsapp",
		Variables: map[string]interface{}{
			workflow.VarChannel: "telegram",
			"nome":              "Ana",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, runs.created)

	assert.Equal(t, "whatsapp", runs.created.State.GetString(workflow.VarChannel),
		"the run's own entry type wins over anything the caller passed")
	assert.Equal(t, "Ana", runs.created.State.GetString("nome"),
		"unrelated caller variables are untouched")
}

// The seeded variable and the branch node must agree, or a flow that prints
// {{channel}} and a flow that branches on it would disagree about the same run.
func TestStartRun_SeededVariableAgreesWithChannelOf(t *testing.T) {
	uc, runs := startRunFixture(t)

	_, err := uc.Execute(workflow.StartRunInput{
		WorkflowID:  "wf-1",
		WorkspaceID: "ws-1",
		EntryID:     "entry-1",
		EntryType:   string(shared.EntryTypeUnofficialWhatsApp),
	})
	require.NoError(t, err)

	assert.Equal(t,
		workflow.ChannelOf(runs.created),
		runs.created.State.GetString(workflow.VarChannel))
}
