package node_executors

import (
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/actor"
	"vozko/domain/stage"
	"vozko/domain/workflow"
)

type stageStoreStub struct {
	stages  map[string]*stage.Stage
	current map[string]*stage.EntryStage
}

func (s *stageStoreStub) FindByID(id string) (*stage.Stage, error) {
	if st, ok := s.stages[id]; ok {
		return st, nil
	}
	return nil, stage.ErrTagNotFound
}

func (s *stageStoreStub) GetEntryStage(entryID, _, _ string) (*stage.EntryStage, error) {
	if es, ok := s.current[entryID]; ok {
		return es, nil
	}
	return nil, stage.ErrEntryTagNotFound
}

type stageMoverStub struct {
	calls []stage.AssignEntryStageInput
	err   error
}

func (m *stageMoverStub) Execute(_ string, in stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	m.calls = append(m.calls, in)
	return &stage.EntryStage{StageID: in.StageID}, m.err
}

type stageBroadcastStub struct{ entries []string }

func (b *stageBroadcastStub) BroadcastStageUpdate(_, entryID, _ string) {
	b.entries = append(b.entries, entryID)
}

func stagePipelines() *stageStoreStub {
	return &stageStoreStub{
		stages: map[string]*stage.Stage{
			"st-new":     {ID: "st-new", WorkspaceID: "ws1", PipelineID: "pl-sales", Name: "Novo"},
			"st-won":     {ID: "st-won", WorkspaceID: "ws1", PipelineID: "pl-sales", Name: "Ganho"},
			"st-foreign": {ID: "st-foreign", WorkspaceID: "ws-other", PipelineID: "pl-x", Name: "Outro"},
		},
		current: map[string]*stage.EntryStage{"e-1": {StageID: "st-new", StageName: "Novo"}},
	}
}

func stageNodeCtx(nodeType workflow.NodeType, config map[string]interface{}, edges []workflow.Edge) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Node:  &workflow.Node{ID: "n1", Type: nodeType, Config: config},
		Graph: &workflow.Graph{Edges: edges},
		Run:   &workflow.WorkflowRun{ID: "run1", WorkflowID: "wf-1", WorkspaceID: "ws1", EntryID: "e-1", EntryType: "unofficial_whatsapp"},
		State: &state,
	}
}

func moveEdges() []workflow.Edge {
	return []workflow.Edge{{Source: "n1", Target: "ok", Label: "sucesso"}, {Source: "n1", Target: "fail", Label: "erro"}}
}

func checkEdges() []workflow.Edge {
	return []workflow.Edge{{Source: "n1", Target: "yes", Label: "true"}, {Source: "n1", Target: "no", Label: "false"}}
}

func TestStageNodesOfferThePipelineStages(t *testing.T) {
	for _, def := range []workflow.NodeDefinition{
		NewMoveStageExecutor(stagePipelines(), &stageMoverStub{}, nil).Definition(),
		NewCheckStageExecutor(stagePipelines()).Definition(),
	} {
		var source string
		for _, f := range def.ConfigSchema {
			if f.Key == "stage_id" {
				source = f.OptionsSource
			}
		}
		require.Equal(t, "stages", source, "%s has no stage picker", def.Type)
		require.True(t, def.Type.Valid(), "%s is not a registered node type", def.Type)
	}
}

// Moving goes through the same use case people and the AI use, so the
// pipeline rule and the timeline entry apply, recorded as the workflow.
func TestMoveStageMovesTheConversationAsTheWorkflow(t *testing.T) {
	mover := &stageMoverStub{}
	screens := &stageBroadcastStub{}
	exec := NewMoveStageExecutor(stagePipelines(), mover, screens)

	res, err := exec.Execute(stageNodeCtx(workflow.NodeTypeActionMoveStage, map[string]interface{}{"stage_id": "st-won"}, moveEdges()))

	require.NoError(t, err)
	require.Equal(t, "ok", res.NextNodeID)
	require.Len(t, mover.calls, 1)
	require.Equal(t, stage.AssignEntryStageInput{StageID: "st-won", EntryID: "e-1", EntryType: "unofficial_whatsapp", ActorID: actor.FormatWorkflow("wf-1")}, mover.calls[0])
	require.Equal(t, []string{"e-1"}, screens.entries)
	require.Equal(t, true, res.Output["changed"])
	require.Equal(t, "Ganho", res.Output["stage_name"])
	require.Equal(t, "Novo", res.Output["from_stage_name"])
}

func TestMoveStageToTheCurrentStageChangesNothing(t *testing.T) {
	mover := &stageMoverStub{}
	res, err := NewMoveStageExecutor(stagePipelines(), mover, nil).
		Execute(stageNodeCtx(workflow.NodeTypeActionMoveStage, map[string]interface{}{"stage_id": "st-new"}, moveEdges()))

	require.NoError(t, err)
	require.Equal(t, "ok", res.NextNodeID)
	require.Empty(t, mover.calls)
	require.Equal(t, false, res.Output["changed"])
}

func TestMoveStageRefusalsLeaveThroughTheErrorHandle(t *testing.T) {
	for name, tc := range map[string]struct {
		stageID string
		err     error
		want    string
	}{
		"other funnel":    {"st-won", stage.ErrStagePipelineMismatch, "funil"},
		"deleted stage":   {"st-gone", nil, "não existe"},
		"other workspace": {"st-foreign", nil, "não existe"},
		"generic failure": {"st-won", stage.ErrEntryNotFound, "falha"},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := NewMoveStageExecutor(stagePipelines(), &stageMoverStub{err: tc.err}, nil).
				Execute(stageNodeCtx(workflow.NodeTypeActionMoveStage, map[string]interface{}{"stage_id": tc.stageID}, moveEdges()))

			require.NoError(t, err)
			require.Equal(t, "fail", res.NextNodeID)
			require.Contains(t, res.Output["error"], tc.want)
		})
	}
}

func TestMoveStageWithoutAStageIsAConfigError(t *testing.T) {
	_, err := NewMoveStageExecutor(stagePipelines(), &stageMoverStub{}, nil).
		Execute(stageNodeCtx(workflow.NodeTypeActionMoveStage, map[string]interface{}{}, moveEdges()))
	require.ErrorIs(t, err, workflow.ErrNodeConfigMissing)
}

func TestCheckStageBranchesOnTheCurrentStage(t *testing.T) {
	store := stagePipelines()
	exec := NewCheckStageExecutor(store)

	res, err := exec.Execute(stageNodeCtx(workflow.NodeTypeConditionCheckStage, map[string]interface{}{"stage_id": "st-new"}, checkEdges()))
	require.NoError(t, err)
	require.Equal(t, "yes", res.NextNodeID)
	require.Equal(t, true, res.Output["matched"])
	require.Equal(t, "Novo", res.Output["current_stage_name"])

	res, err = exec.Execute(stageNodeCtx(workflow.NodeTypeConditionCheckStage, map[string]interface{}{"stage_id": "st-won"}, checkEdges()))
	require.NoError(t, err)
	require.Equal(t, "no", res.NextNodeID)

	delete(store.current, "e-1")
	res, err = exec.Execute(stageNodeCtx(workflow.NodeTypeConditionCheckStage, map[string]interface{}{"stage_id": "st-new"}, checkEdges()))
	require.NoError(t, err)
	require.Equal(t, "no", res.NextNodeID, "a conversation in no stage is not in the chosen one")
}
