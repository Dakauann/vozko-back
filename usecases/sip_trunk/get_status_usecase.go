package sip_trunk_usecase

import (
	"time"

	"vozko/domain/sip_trunk"
)

type getStatusUseCase struct {
	trunkManager sip_trunk.TrunkManager
}

func NewGetStatusUseCase(trunkManager sip_trunk.TrunkManager) sip_trunk.GetStatusUseCase {
	return &getStatusUseCase{trunkManager: trunkManager}
}

func (uc *getStatusUseCase) Execute(trunkID string) (*sip_trunk.SIPTrunkStatusUpdate, error) {
	if uc.trunkManager == nil {
		return &sip_trunk.SIPTrunkStatusUpdate{
			TrunkID:   trunkID,
			Status:    sip_trunk.RegistrationStatusPending,
			Error:     "trunk manager not available",
			Timestamp: time.Now(),
		}, nil
	}
	return uc.trunkManager.GetTrunkStatus(trunkID)
}
