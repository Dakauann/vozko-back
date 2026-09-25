package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/stage"
	"vozko/domain/workflow"
	opportunity_usecase "vozko/usecases/opportunity"
)

type dealFunnelsStub struct {
	err error
}

func (s dealFunnelsStub) PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if s.err != nil {
		return nil, s.err
	}
	if workspaceID != "ws1" || pipelineID != "pl-deals" {
		return nil, opportunity_usecase.ErrPipelineNotFound
	}
	return []*stage.Stage{{ID: "st-deal-new", PipelineID: "pl-deals"}, {ID: "st-deal-won", PipelineID: "pl-deals", IsWon: true}}, nil
}

func TestActivationChecksTheDealFunnelAndItsStage(t *testing.T) {
	v := &dealValidator{funnels: dealFunnelsStub{}, workspaceID: "ws1"}
	cases := []struct {
		name   string
		config map[string]interface{}
		want   error
	}{
		{"funnel and stage of the workspace", map[string]interface{}{"pipeline_id": "pl-deals", "stage_id": "st-deal-won"}, nil},
		{"funnel without a stage", map[string]interface{}{"pipeline_id": "pl-deals"}, nil},
		{"nothing chosen yet", map[string]interface{}{}, nil},
		{"another workspace's funnel", map[string]interface{}{"pipeline_id": "pl-foreign"}, workflow.ErrNodeInvalidOpportunityPipeline},
		{"a stage of another funnel", map[string]interface{}{"pipeline_id": "pl-deals", "stage_id": "st-conversation"}, workflow.ErrNodeInvalidStageID},
	}
	for _, nodeType := range []workflow.NodeType{workflow.NodeTypeActionManageOpportunity, workflow.NodeTypeConditionCheckOpportunity} {
		for _, tc := range cases {
			err := v.Validate(&workflow.Node{ID: "n1", Type: nodeType, Config: tc.config})
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("%s, %s: err = %v, want %v", nodeType, tc.name, err, tc.want)
			}
		}
	}
}

func TestActivationDoesNotBlameTheFunnelForAnOutage(t *testing.T) {
	outage := errors.New("connection reset")
	v := &dealValidator{funnels: dealFunnelsStub{err: outage}, workspaceID: "ws1"}
	err := v.Validate(&workflow.Node{ID: "n1", Type: workflow.NodeTypeActionManageOpportunity, Config: map[string]interface{}{"pipeline_id": "pl-deals"}})
	if !errors.Is(err, outage) || errors.Is(err, workflow.ErrNodeInvalidOpportunityPipeline) {
		t.Fatalf("err = %v, want the outage itself", err)
	}
}

func TestActivationIgnoresOtherNodesForDeals(t *testing.T) {
	v := &dealValidator{funnels: dealFunnelsStub{}, workspaceID: "ws1"}
	if err := v.Validate(&workflow.Node{ID: "n1", Type: workflow.NodeTypeActionMoveStage, Config: map[string]interface{}{"pipeline_id": "x"}}); err != nil {
		t.Fatalf("a move-stage node was checked as a deal node: %v", err)
	}
}
