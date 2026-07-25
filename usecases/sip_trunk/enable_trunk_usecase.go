package sip_trunk_usecase

import (
	"log"

	"vozko/domain/sip_trunk"
)

type enableTrunkUseCase struct {
	repo         sip_trunk.Repository
	trunkManager sip_trunk.TrunkManager
}

func NewEnableUseCase(repo sip_trunk.Repository, trunkManager sip_trunk.TrunkManager) sip_trunk.EnableUseCase {
	return &enableTrunkUseCase{repo: repo, trunkManager: trunkManager}
}

func (uc *enableTrunkUseCase) Execute(id string, enabled bool, actor sip_trunk.Actor) (*sip_trunk.SIPTrunk, error) {
	trunk, err := uc.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if err := trunk.CanBeModifiedBy(actor); err != nil {
		return nil, err
	}

	trunk.Enabled = enabled

	if err := uc.repo.Update(trunk); err != nil {
		return nil, err
	}

	if uc.trunkManager != nil {
		go func() {
			if enabled {
				log.Printf("[SIPTrunk] Trunk %s enabled, triggering registration...", id)
				if err := uc.trunkManager.RegisterTrunk(trunk); err != nil {
					log.Printf("[SIPTrunk] Failed to register trunk %s: %v", id, err)
				} else {
					log.Printf("[SIPTrunk] Trunk %s registration initiated successfully", id)
				}
			} else {
				log.Printf("[SIPTrunk] Trunk %s disabled, unregistering...", id)
				if err := uc.trunkManager.UnregisterTrunk(id); err != nil {
					log.Printf("[SIPTrunk] Failed to unregister trunk %s: %v", id, err)
				} else {
					log.Printf("[SIPTrunk] Trunk %s unregistered successfully", id)
				}
			}
		}()
	}

	return trunk, nil
}
