package branchinfra

import (
	"log"
	"sync"

	"vozko/domain/branch"
	dialer_domain "vozko/domain/dialer"
)

// PresenceRegistrar is the glue that turns "a phone registered" into "a branch is
// a transfer target". It implements branch.BranchPresenceListener: when a branch
// comes online it constructs a branchSession and registers it in the shared
// DialerSessionRegistry (so the existing FindByUser / ListAvailable / offer paths
// resolve it); when it goes offline it deregisters it. This is the ONLY new wiring
// needed to make transfers reach a SIP phone, on top of the reused transfer engine.
type PresenceRegistrar struct {
	registry dialer_domain.DialerSessionRegistry
	ring     branch.BranchRing
	bridge   branch.BranchMediaBridge
	logger   *log.Logger

	mu     sync.Mutex
	active map[string]activeReg // branchID -> registration
}

type activeReg struct {
	session    *branchSession
	deregister func()
}

func NewPresenceRegistrar(registry dialer_domain.DialerSessionRegistry, ring branch.BranchRing, bridge branch.BranchMediaBridge, logger *log.Logger) *PresenceRegistrar {
	if logger == nil {
		logger = log.Default()
	}
	return &PresenceRegistrar{
		registry: registry,
		ring:     ring,
		bridge:   bridge,
		logger:   logger,
		active:   make(map[string]activeReg),
	}
}

func (p *PresenceRegistrar) OnBranchReachable(b branch.RegisteredBranch) {
	if b.BranchID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.active[b.BranchID]; ok {
		// Already online (a re-REGISTER refresh); the session persists so any live
		// reservation/call is untouched.
		return
	}
	sess := newBranchSession(b, p.ring, p.bridge, p.logger)
	// Broadcast presence when this branch answers or hangs up so the roster shows
	// the member busy the moment they pick up on the phone and free when they end.
	ws := b.WorkspaceID
	sess.SetPresenceCallback(func() { p.registry.NotifyPresenceChanged(ws) })
	deregister, err := p.registry.Register(sess)
	if err != nil {
		p.logger.Printf("[branch-presence] register session for branch %s failed: %v", b.BranchID, err)
		return
	}
	p.active[b.BranchID] = activeReg{session: sess, deregister: deregister}
}

func (p *PresenceRegistrar) OnBranchUnreachable(branchID string) {
	if branchID == "" {
		return
	}
	p.mu.Lock()
	reg, ok := p.active[branchID]
	if ok {
		delete(p.active, branchID)
	}
	p.mu.Unlock()
	if ok {
		reg.deregister()
	}
}

var _ branch.BranchPresenceListener = (*PresenceRegistrar)(nil)
