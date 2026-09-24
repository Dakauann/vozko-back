package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/stage"
	"vozko/domain/workflow"
)

type stageLookupStub map[string]*stage.Stage

func (s stageLookupStub) FindByID(id string) (*stage.Stage, error) {
	if st, ok := s[id]; ok {
		return st, nil
	}
	return nil, stage.ErrTagNotFound
}

func TestActivationRefusesAStageOutsideTheWorkspace(t *testing.T) {
	v := &stageValidator{repo: stageLookupStub{
		"st-mine":  {ID: "st-mine", WorkspaceID: "ws1"},
		"st-other": {ID: "st-other", WorkspaceID: "ws2"},
	}, workspaceID: "ws1"}

	for _, nodeType := range []workflow.NodeType{workflow.NodeTypeActionMoveStage, workflow.NodeTypeConditionCheckStage} {
		for stageID, wantErr := range map[string]bool{"st-mine": false, "st-other": true, "st-gone": true, "": false} {
			err := v.Validate(&workflow.Node{ID: "n1", Type: nodeType, Config: map[string]interface{}{"stage_id": stageID}})
			if got := errors.Is(err, workflow.ErrNodeInvalidStageID); got != wantErr {
				t.Errorf("%s with stage %q: err = %v, want refused = %v", nodeType, stageID, err, wantErr)
			}
		}
	}
}
