package sip_trunk_usecase

import (
	"log"

	"vozko/domain/sip_trunk"

	"github.com/google/uuid"
)

type createTrunkUseCase struct {
	repo            sip_trunk.Repository
	trunkManager    sip_trunk.TrunkManager
	maxPerWorkspace int
	hostGuard       sip_trunk.HostGuard
}

// NewCreateUseCase builds the create use case. maxPerWorkspace caps how many
// trunks a non-admin workspace may own (<= 0 disables the cap); admins are
// never capped so platform/global trunks remain unconstrained. hostGuard
// (nil-safe) rejects hosts that resolve to internal addresses.
func NewCreateUseCase(repo sip_trunk.Repository, trunkManager sip_trunk.TrunkManager, maxPerWorkspace int, hostGuard sip_trunk.HostGuard) sip_trunk.CreateUseCase {
	return &createTrunkUseCase{repo: repo, trunkManager: trunkManager, maxPerWorkspace: maxPerWorkspace, hostGuard: hostGuard}
}

func (uc *createTrunkUseCase) Execute(input sip_trunk.CreateTrunkInput) (*sip_trunk.SIPTrunk, error) {
	// Owner-visibility baseline is non-global; only an admin actor may publish
	// globally. This keeps the rule in the domain rather than the handler.
	trunk := sip_trunk.NewSIPTrunk(
		uuid.New().String(),
		input.WorkspaceID,
		input.Name,
		input.Description,
		input.Host,
		input.Port,
		input.Username,
		input.Password,
		input.Domain,
		input.Transport,
		input.TrunkType,
		input.IsRotational,
		input.PhoneNumber,
		false,
	)
	if err := trunk.ApplyGlobalVisibility(input.IsGloballyVisible, input.Actor); err != nil {
		return nil, err
	}

	input.Advanced.ApplyTo(trunk)

	if err := trunk.Validate(); err != nil {
		return nil, err
	}

	// Resolve-time SSRF guard: reject a host that resolves to an internal address
	// even though it passed the IP-literal validation.
	if uc.hostGuard != nil {
		for _, h := range trunk.OutboundHosts() {
			if uc.hostGuard.ResolvesToBlocked(h) {
				return nil, sip_trunk.ErrTrunkHostBlocked
			}
		}
	}

	// Per-workspace self-service cap (admins bypass). Enforced before persisting
	// so a tenant cannot exhaust the shared SIP listener-port pool.
	if !input.Actor.IsAdmin && uc.maxPerWorkspace > 0 {
		count, err := uc.repo.CountByWorkspace(trunk.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if count >= int64(uc.maxPerWorkspace) {
			return nil, sip_trunk.ErrTrunkLimitReached
		}
	}

	if err := uc.repo.Create(trunk); err != nil {
		return nil, err
	}

	if uc.trunkManager != nil && trunk.Enabled {
		log.Printf("[SIPTrunk] Trunk %s created, triggering registration with %s...", trunk.ID, trunk.Host)
		go func() {
			if err := uc.trunkManager.RegisterTrunk(trunk); err != nil {
				log.Printf("[SIPTrunk] Failed to register trunk %s: %v", trunk.ID, err)
			} else {
				log.Printf("[SIPTrunk] Trunk %s registration initiated successfully", trunk.ID)
			}
		}()
	} else if uc.trunkManager == nil {
		log.Printf("[SIPTrunk] Warning: Trunk %s created but TrunkManager is nil - registration skipped", trunk.ID)
	}

	return trunk, nil
}
