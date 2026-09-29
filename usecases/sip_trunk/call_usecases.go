package sip_trunk_usecase

import (
	"context"

	"vozko/domain/sip_trunk"
)

type HangupCallUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewHangupCallUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *HangupCallUseCase {
	return &HangupCallUseCase{repo: repo, engine: engine}
}

func (uc *HangupCallUseCase) Execute(ctx context.Context, workspaceID, trunkID, callID string) error {
	trunk, err := uc.repo.FindInWorkspace(ctx, workspaceID, trunkID)
	if err != nil {
		return err
	}
	return uc.engine.Hangup(ctx, trunk.ID, callID)
}

type ListCallsUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewListCallsUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *ListCallsUseCase {
	return &ListCallsUseCase{repo: repo, engine: engine}
}

func (uc *ListCallsUseCase) Execute(ctx context.Context, workspaceID, trunkID string) ([]sip_trunk.ActiveCall, error) {
	trunk, err := uc.repo.FindInWorkspace(ctx, workspaceID, trunkID)
	if err != nil {
		return nil, err
	}
	return uc.engine.ActiveCalls(trunk.ID), nil
}
