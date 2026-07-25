package sip_trunk_usecase

import (
	"log"

	"vozko/domain/sip_trunk"
)

type updateTrunkUseCase struct {
	repo         sip_trunk.Repository
	trunkManager sip_trunk.TrunkManager
	hostGuard    sip_trunk.HostGuard
}

func NewUpdateUseCase(repo sip_trunk.Repository, trunkManager sip_trunk.TrunkManager, hostGuard sip_trunk.HostGuard) sip_trunk.UpdateUseCase {
	return &updateTrunkUseCase{repo: repo, trunkManager: trunkManager, hostGuard: hostGuard}
}

func (uc *updateTrunkUseCase) Execute(input sip_trunk.UpdateTrunkInput) (*sip_trunk.SIPTrunk, error) {
	trunk, err := uc.repo.FindByID(input.ID)
	if err != nil {
		return nil, err
	}

	// Authorization invariant lives here (not the handler): non-admins may only
	// modify a trunk they own and never a globally-visible one.
	if err := trunk.CanBeModifiedBy(input.Actor); err != nil {
		return nil, err
	}

	if input.Name != nil {
		trunk.Name = *input.Name
	}
	if input.Description != nil {
		trunk.Description = *input.Description
	}
	if input.Host != nil {
		trunk.Host = *input.Host
	}
	if input.Port != nil {
		trunk.Port = *input.Port
	}
	if input.Username != nil {
		trunk.Username = *input.Username
	}
	if input.Password != nil {
		trunk.Password = *input.Password
	}
	if input.Domain != nil {
		trunk.Domain = *input.Domain
	}
	if input.Transport != nil {
		trunk.Transport = *input.Transport
	}
	if input.TrunkType != nil {
		trunk.TrunkType = *input.TrunkType
	}
	if input.IsRotational != nil {
		trunk.IsRotational = *input.IsRotational
	}
	if input.PhoneNumber != nil {
		trunk.PhoneNumber = *input.PhoneNumber
	}
	if input.IsGloballyVisible != nil {
		if err := trunk.ApplyGlobalVisibility(*input.IsGloballyVisible, input.Actor); err != nil {
			return nil, err
		}
	}

	if input.Advanced != nil {
		input.Advanced.ApplyTo(trunk)
	}

	if err := trunk.Validate(); err != nil {
		return nil, err
	}

	// Resolve-time SSRF guard on the (possibly changed) connection targets.
	if uc.hostGuard != nil {
		for _, h := range trunk.OutboundHosts() {
			if uc.hostGuard.ResolvesToBlocked(h) {
				return nil, sip_trunk.ErrTrunkHostBlocked
			}
		}
	}

	if err := uc.repo.Update(trunk); err != nil {
		return nil, err
	}

	if uc.trunkManager != nil {
		log.Printf("[SIPTrunk] Trunk %s updated, refreshing registration...", trunk.ID)
		go func() {
			if err := uc.trunkManager.RefreshTrunk(trunk); err != nil {
				log.Printf("[SIPTrunk] Failed to refresh trunk %s: %v", trunk.ID, err)
			} else {
				log.Printf("[SIPTrunk] Trunk %s refresh initiated successfully", trunk.ID)
			}
		}()
	}

	return trunk, nil
}
