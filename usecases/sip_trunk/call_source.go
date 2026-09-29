package sip_trunk_usecase

import (
	"context"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	"vozko/domain/sip_trunk"
)

type CallPermissions interface {
	MayCallThroughTrunks(userID, workspaceID string, isAdmin bool) bool
}

type CallSource struct {
	planner sip_trunk.CallPlanner
	engine  sip_trunk.Engine
}

var _ conversation.CallSource = (*CallSource)(nil)

func NewCallSource(planner sip_trunk.CallPlanner, engine sip_trunk.Engine) *CallSource {
	return &CallSource{planner: planner, engine: engine}
}

func (s *CallSource) Name() string { return "sip_trunk" }

func (s *CallSource) Dial(ctx context.Context, input conversation.CallDialInput) (conversation.CRMCall, error) {
	if input.TrunkID == "" {
		return nil, sip_trunk.ErrTrunkNotFound
	}
	plan, err := s.planner.Plan(ctx, sip_trunk.CallPlanInput{
		WorkspaceID: input.WorkspaceID,
		UserID:      input.UserID,
		IsAdmin:     input.IsAdmin,
		TrunkID:     input.TrunkID,
		PhoneNumber: input.PhoneNumber,
	})
	if err != nil {
		return nil, err
	}
	trunk, ok := plan.Chosen()
	if !ok {
		return nil, sip_trunk.ErrTrunkNotFound
	}
	call := newTrunkCall(cdr.NewSIPOutboundCallID(), trunk.ID, s.engine)
	go call.dial(plan.PhoneNumber)
	return call, nil
}
