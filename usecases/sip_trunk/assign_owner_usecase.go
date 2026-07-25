package sip_trunk_usecase

import (
	"time"

	"vozko/domain/sip_trunk"
)

type assignOwnerUseCase struct {
	repo sip_trunk.Repository
}

func NewAssignOwnerUseCase(repo sip_trunk.Repository) sip_trunk.AssignOwnerUseCase {
	return &assignOwnerUseCase{repo: repo}
}

func (uc *assignOwnerUseCase) Execute(trunkID, workspaceID, assignedByUserID string) (*sip_trunk.SIPTrunk, error) {
	trunk, err := uc.repo.FindByID(trunkID)
	if err != nil {
		return nil, err
	}

	if err := trunk.AssignOwner(workspaceID, assignedByUserID, time.Now()); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(trunk); err != nil {
		return nil, err
	}

	return trunk, nil
}
