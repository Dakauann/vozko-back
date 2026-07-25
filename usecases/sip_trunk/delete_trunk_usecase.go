package sip_trunk_usecase

import (
	"log"

	"vozko/domain/sip_trunk"
)

type deleteTrunkUseCase struct {
	repo         sip_trunk.Repository
	trunkManager sip_trunk.TrunkManager
}

func NewDeleteUseCase(repo sip_trunk.Repository, trunkManager sip_trunk.TrunkManager) sip_trunk.DeleteUseCase {
	return &deleteTrunkUseCase{repo: repo, trunkManager: trunkManager}
}

func (uc *deleteTrunkUseCase) Execute(id string, actor sip_trunk.Actor) error {
	trunk, err := uc.repo.FindByID(id)
	if err != nil {
		return err
	}
	if err := trunk.CanBeModifiedBy(actor); err != nil {
		return err
	}

	if uc.trunkManager != nil {
		log.Printf("[SIPTrunk] Deleting trunk %s, unregistering first...", id)
		if err := uc.trunkManager.UnregisterTrunk(id); err != nil {
			log.Printf("[SIPTrunk] Failed to unregister trunk %s (may not be registered): %v", id, err)
		}
	}

	return uc.repo.Delete(id)
}
