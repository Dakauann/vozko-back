package sip_trunk_usecase

import (
	"context"

	"vozko/domain/sip_trunk"
)

type CallPlanner struct {
	repo        sip_trunk.Repository
	engine      sip_trunk.Engine
	permissions CallPermissions
}

var _ sip_trunk.CallPlanner = (*CallPlanner)(nil)

func NewCallPlanner(repo sip_trunk.Repository, engine sip_trunk.Engine, permissions CallPermissions) *CallPlanner {
	return &CallPlanner{repo: repo, engine: engine, permissions: permissions}
}

func (p *CallPlanner) Plan(ctx context.Context, input sip_trunk.CallPlanInput) (*sip_trunk.CallPlan, error) {
	if p.permissions == nil || !p.permissions.MayCallThroughTrunks(input.UserID, input.WorkspaceID, input.IsAdmin) {
		return nil, sip_trunk.ErrCallNotPermitted
	}
	number, err := sip_trunk.NormalizeDialString(input.PhoneNumber)
	if err != nil {
		return nil, err
	}
	trunks, err := p.dialableTrunks(ctx, input)
	if err != nil {
		return nil, err
	}
	return &sip_trunk.CallPlan{PhoneNumber: number, Trunks: trunks}, nil
}

func (p *CallPlanner) dialableTrunks(ctx context.Context, input sip_trunk.CallPlanInput) ([]sip_trunk.TrunkChoice, error) {
	if input.TrunkID != "" {
		trunk, err := p.repo.FindInWorkspace(ctx, input.WorkspaceID, input.TrunkID)
		if err != nil {
			return nil, err
		}
		if err := p.dialability(trunk); err != nil {
			return nil, err
		}
		return []sip_trunk.TrunkChoice{choiceOf(trunk)}, nil
	}
	trunks, err := p.repo.ListByWorkspace(ctx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	choices := make([]sip_trunk.TrunkChoice, 0, len(trunks))
	for _, trunk := range trunks {
		if p.dialability(trunk) == nil {
			choices = append(choices, choiceOf(trunk))
		}
	}
	if len(choices) == 0 {
		return nil, sip_trunk.ErrNoDialableTrunk
	}
	return choices, nil
}

func (p *CallPlanner) dialability(trunk *sip_trunk.SIPTrunk) error {
	if !trunk.Enabled {
		return sip_trunk.ErrTrunkDisabled
	}
	if !trunk.TrunkType.Supports(sip_trunk.CallDirectionOutbound) {
		return sip_trunk.ErrTrunkCannotDial
	}
	if status, ok := p.engine.TrunkStatus(trunk.ID); !ok || status.Status != sip_trunk.RegistrationStatusRegistered {
		return sip_trunk.ErrTrunkNotRegistered
	}
	return nil
}

func choiceOf(trunk *sip_trunk.SIPTrunk) sip_trunk.TrunkChoice {
	return sip_trunk.TrunkChoice{ID: trunk.ID, Name: trunk.Name}
}
