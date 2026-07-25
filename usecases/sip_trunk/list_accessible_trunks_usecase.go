package sip_trunk_usecase

import (
	"vozko/domain/sip_trunk"
)

type listAccessibleTrunksUseCase struct {
	repo sip_trunk.Repository
}

func NewListAccessibleUseCase(repo sip_trunk.Repository) sip_trunk.ListAccessibleUseCase {
	return &listAccessibleTrunksUseCase{repo: repo}
}

func (uc *listAccessibleTrunksUseCase) Execute(workspaceID string, grantedTrunkIDs []string, page, pageSize int) ([]*sip_trunk.SIPTrunk, int64, error) {
	return uc.repo.FindAccessible(workspaceID, grantedTrunkIDs, page, pageSize)
}
