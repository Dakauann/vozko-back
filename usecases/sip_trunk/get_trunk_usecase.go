package sip_trunk_usecase

import (
	"vozko/domain/sip_trunk"
)

type getTrunkUseCase struct {
	repo sip_trunk.Repository
}

func NewGetUseCase(repo sip_trunk.Repository) sip_trunk.GetUseCase {
	return &getTrunkUseCase{repo: repo}
}

func (uc *getTrunkUseCase) Execute(id string) (*sip_trunk.SIPTrunk, error) {
	return uc.repo.FindByID(id)
}
