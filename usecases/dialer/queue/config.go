package queue

import (
	"context"
	"time"

	dialer "vozko/domain/dialer"
	wsc "vozko/domain/workspace_config"
)

// WorkspaceConfigReader is the narrow read side of the workspace config the policy
// resolver needs.
type WorkspaceConfigReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

// ConfigPolicyResolver resolves a workspace's queue policy from its WorkspaceConfig.
// v1 is workspace-level: every target in the workspace shares one policy (the same
// bounds). Per-department overrides can layer on later without touching the director
// (this stays the single place a policy is resolved).
type ConfigPolicyResolver struct {
	cfg WorkspaceConfigReader
}

func NewConfigPolicyResolver(cfg WorkspaceConfigReader) *ConfigPolicyResolver {
	return &ConfigPolicyResolver{cfg: cfg}
}

var _ PolicyResolver = (*ConfigPolicyResolver)(nil)

func (r *ConfigPolicyResolver) Resolve(ctx context.Context, workspaceID string, target dialer.QueueTarget) dialer.QueuePolicy {
	if r == nil || r.cfg == nil {
		return dialer.QueuePolicy{Enabled: false}
	}
	c, err := r.cfg.GetByWorkspaceID(ctx, workspaceID)
	if err != nil || c == nil {
		// Fail closed: a config read error must never enable an unbounded queue.
		return dialer.QueuePolicy{Enabled: false}
	}
	overflow := dialer.QueueOverflowAction(c.QueueOverflow)
	if !overflow.Valid() {
		overflow = dialer.QueueOverflowHangup
	}
	base := dialer.QueuePolicy{
		// The director Normalizes (defaults + hard caps), so raw config values are safe.
		MaxWait:   time.Duration(c.QueueMaxWaitSeconds) * time.Second,
		MaxLength: c.QueueMaxLength,
		Overflow:  overflow,
	}

	// Department / workspace DISTRIBUTION always rings the pool through the ONE queue
	// engine (this replaced the separate roulette). The QueueEnabled toggle only
	// chooses HOW: on -> rrmemory ACD hold; off -> a single ringall pass with no hold
	// (ring the team, overflow if none answer). A CAMP-ON to one SPECIFIC agent stays
	// gated by the toggle: off means the transfer returns busy synchronously, never a
	// parked wait for a colleague.
	switch target.Kind {
	case dialer.QueueTargetAgent:
		base.Enabled = c.QueueEnabled
		base.Strategy = dialer.QueueStrategyRRMemory
	default: // department, workspace
		base.Enabled = true
		if c.QueueEnabled {
			base.Strategy = dialer.QueueStrategyRRMemory
		} else {
			base.Strategy = dialer.QueueStrategyRingAll
		}
	}
	return base
}
