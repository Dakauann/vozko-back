package sip_trunk_usecase

import (
	"vozko/domain/sip_trunk"
)

type listTrunksUseCase struct {
	repo sip_trunk.Repository
}

func NewListUseCase(repo sip_trunk.Repository) sip_trunk.ListUseCase {
	return &listTrunksUseCase{repo: repo}
}

func (uc *listTrunksUseCase) Execute(page, pageSize int) ([]*sip_trunk.SIPTrunk, int64, error) {
	return uc.repo.FindAll(page, pageSize)
}
