package sip_trunk_usecase

import "vozko/domain/sip_trunk"

type listTrunksByIDsUseCase struct {
	repo sip_trunk.Repository
}

func NewListByIDsUseCase(repo sip_trunk.Repository) sip_trunk.ListByIDsUseCase {
	return &listTrunksByIDsUseCase{repo: repo}
}

func (uc *listTrunksByIDsUseCase) Execute(ids []string) ([]*sip_trunk.SIPTrunk, error) {
	return uc.repo.FindByIDs(ids)
}
