package container

import (
	"context"
	"log"
	"time"

	"vozko/domain/branch"
	branchinfra "vozko/infra/branch"
	voipinfra "vozko/infra/voip"
	branch_usecase "vozko/usecases/branch"
)

// initBranchRegistrar wires and starts the single-VPS branch SIP registrar. It
// must run AFTER initDialerTransferStack so it can register branch sessions into
// the SAME DialerSessionRegistry the transfer engine uses (c.services.dialerSessions),
// which is what makes a registered phone a transfer target. Auto-enables when
// PUBLIC_SIP_HOST is set (off in dev/CI where there is no public host).
func (c *Container) initBranchRegistrar() {
	// Auto-enables when a public SIP host is configured; stays off in dev/CI.
	if c.cfg.PublicSIPHost == "" {
		return
	}

	// Signed digest nonces (replay-resistant, source-bound, 32s lifetime). A random
	// per-process secret is right for a single VPS: a restart just forces phones to
	// re-challenge. For multi-replica, seed this from shared config instead so a nonce
	// issued by one replica verifies on another.
	nonces := branch.NewRandomNonceService(time.Now)

	// AOR bindings live in Redis (the durable, shared location service), NOT in process,
	// so they survive an app restart and the registrar can rehydrate routing from them
	// on boot instead of leaving a gap until each phone re-REGISTERs.
	bindingStore := branchinfra.NewRedisBindingStore(c.redisProvider.SharedState(), time.Now)
	route := branch_usecase.NewRouteToBranchUseCase(c.repositories.branch, bindingStore)

	// The registrar is itself the BranchRing; presence rings through it; the
	// register use case notifies presence. Break the cycle with Wire (below).
	registrar := voipinfra.NewBranchRegistrar(
		voipinfra.BranchRegistrarConfig{
			PublicHost: c.cfg.PublicSIPHost,
			ListenPort: c.cfg.BranchSIPListenPort,
			Realm:      c.cfg.SIPRealm,
			// Share the trunk RTP window deliberately (diago's allocator retries binds, so
			// no collision) and let the registrar fail-loud if it is somehow unset.
			RTPPortStart: c.cfg.SIPTrunkRTPPortStart,
			RTPPortEnd:   c.cfg.SIPTrunkRTPPortEnd,
		},
		nil, // handle: wired below
		bindingStore,
		route,
		nil, // presence: wired below
		nil,
	)

	// The registrar is the BranchRing AND the BranchMediaBridge: presence rings
	// through it and branch sessions bridge media through it. It also drives the
	// reused transfer engine once a phone answers (Accept -> SwapBlind -> AttachLeg
	// -> StartBridge). initBranchRegistrar runs after initDialerTransferStack, so the
	// transfer use case is available here.
	presence := branchinfra.NewPresenceRegistrar(c.services.dialerSessions, registrar, registrar, nil)
	handle := branch_usecase.NewHandleRegisterUseCase(c.repositories.branch, bindingStore, presence, c.cfg.SIPRealm, time.Now, nonces)
	registrar.Wire(handle, presence)
	registrar.SetTransferUseCase(c.services.dialerTransferUC)
	registrar.SetStatusUpdater(c.repositories.branch)

	// Per-source-IP REGISTER throttle: blocks a flood / enumeration spray (which would
	// otherwise hammer Postgres via unknown-user lookups) while leaving ample headroom
	// for legitimate phones (a NAT with dozens of phones refreshing every ~60-120s).
	if c.services.rateLimiterFactory != nil {
		registrar.SetRateLimiter(c.services.rateLimiterFactory("branch_register", 120, time.Minute))
	}

	c.rehydrateBranchRegistrations(bindingStore, presence)

	if err := registrar.Start(context.Background()); err != nil {
		log.Printf("[branch-registrar] failed to start: %v", err)
		return
	}
	c.services.branchRegistrar = registrar
	log.Printf("[branch-registrar] started on %s:%d", c.cfg.PublicSIPHost, c.cfg.BranchSIPListenPort)
}

// rehydrateBranchRegistrations rebuilds routing from the durable Redis bindings on boot.
// It first marks every branch offline (any `registered` row is stale after a restart),
// then re-registers each branch that still has a live binding in Redis, so a branch whose
// phone is still registered becomes routable IMMEDIATELY on boot instead of after its
// next refresh REGISTER. If Redis was also cold (no bindings), nothing is rehydrated and
// the branches correctly stay offline until their phones re-REGISTER.
func (c *Container) rehydrateBranchRegistrations(store branch.BindingStore, presence branch.BranchPresenceListener) {
	if n, err := c.repositories.branch.ResetLiveRegistrations(); err != nil {
		log.Printf("[branch-registrar] reset stale registrations on boot failed: %v", err)
	} else if n > 0 {
		log.Printf("[branch-registrar] marked %d branch(es) offline on boot (will rehydrate from Redis)", n)
	}

	rehydrator, ok := store.(branchinfra.BindingRehydrator)
	if !ok {
		return
	}
	live, err := rehydrator.ListAllLive()
	if err != nil {
		log.Printf("[branch-registrar] rehydrate: list live bindings failed: %v", err)
		return
	}
	seen := make(map[string]struct{}, len(live))
	for _, b := range live {
		if _, done := seen[b.BranchID]; done || b.BranchID == "" {
			continue // one branch can have several contacts; register the session once
		}
		seen[b.BranchID] = struct{}{}
		presence.OnBranchReachable(branch.RegisteredBranch{
			BranchID: b.BranchID, SIPUser: b.SIPUser, WorkspaceID: b.WorkspaceID, UserID: b.UserID,
		})
		_ = c.repositories.branch.UpdateRegistrationStatus(b.BranchID, branch.RegistrationStatusRegistered)
	}
	if len(seen) > 0 {
		log.Printf("[branch-registrar] rehydrated %d branch(s) from Redis on boot", len(seen))
	}
}
