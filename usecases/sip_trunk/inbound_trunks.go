package sip_trunk_usecase

import (
	"context"
	"errors"

	"vozko/domain/sip_trunk"
)

type InboundTrunks struct {
	repo sip_trunk.Repository
}

func NewInboundTrunks(repo sip_trunk.Repository) *InboundTrunks {
	return &InboundTrunks{repo: repo}
}

func (t *InboundTrunks) ReceivesCalls(workspaceID, trunkID string) (bool, error) {
	trunk, err := t.repo.FindInWorkspace(context.Background(), workspaceID, trunkID)
	if errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return trunk.TrunkType.Supports(sip_trunk.CallDirectionInbound), nil
}
