package queue

import (
	"context"
	"errors"
	"testing"

	dialer "vozko/domain/dialer"
	wsc "vozko/domain/workspace_config"
)

type fakeConfigReader struct {
	cfg *wsc.WorkspaceConfig
	err error
}

func (f fakeConfigReader) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return f.cfg, f.err
}

func resolve(t *testing.T, cfg *wsc.WorkspaceConfig, err error, target dialer.QueueTarget) dialer.QueuePolicy {
	t.Helper()
	r := NewConfigPolicyResolver(fakeConfigReader{cfg: cfg, err: err})
	return r.Resolve(context.Background(), "ws-1", target).Normalized()
}

// A camp-on to ONE specific agent is gated by the toggle: off means "do not queue",
// so the transfer returns busy synchronously (never a parked wait for a colleague).
func TestResolver_AgentTargetGatedByToggle(t *testing.T) {
	on := resolve(t, &wsc.WorkspaceConfig{QueueEnabled: true}, nil, dialer.QueueTarget{Kind: dialer.QueueTargetAgent, ID: "u1"})
	if !on.Enabled || on.Strategy != dialer.QueueStrategyRRMemory {
		t.Fatalf("agent + toggle on: want enabled rrmemory, got %+v", on)
	}
	off := resolve(t, &wsc.WorkspaceConfig{QueueEnabled: false}, nil, dialer.QueueTarget{Kind: dialer.QueueTargetAgent, ID: "u1"})
	if off.Enabled {
		t.Fatalf("agent + toggle off: camp-on must be disabled, got %+v", off)
	}
}

// Department / workspace DISTRIBUTION always rings the pool through the queue; the
// toggle only chooses HOW: on => rrmemory hold, off => a single ringall pass.
func TestResolver_DistributionAlwaysEnabled_StrategyFollowsToggle(t *testing.T) {
	for _, kind := range []dialer.QueueTargetKind{dialer.QueueTargetDepartment, dialer.QueueTargetWorkspace} {
		target := dialer.QueueTarget{Kind: kind}
		if kind == dialer.QueueTargetDepartment {
			target.ID = "dept-1"
		}

		on := resolve(t, &wsc.WorkspaceConfig{QueueEnabled: true}, nil, target)
		if !on.Enabled || on.Strategy != dialer.QueueStrategyRRMemory {
			t.Fatalf("%s + toggle on: want enabled rrmemory (hold), got %+v", kind, on)
		}
		off := resolve(t, &wsc.WorkspaceConfig{QueueEnabled: false}, nil, target)
		if !off.Enabled || off.Strategy != dialer.QueueStrategyRingAll {
			t.Fatalf("%s + toggle off: want enabled ringall (no hold), got %+v", kind, off)
		}
	}
}

// A config read error fails CLOSED for every target: never enable an unbounded queue.
func TestResolver_ConfigErrorFailsClosed(t *testing.T) {
	for _, target := range []dialer.QueueTarget{
		{Kind: dialer.QueueTargetWorkspace},
		{Kind: dialer.QueueTargetDepartment, ID: "d"},
		{Kind: dialer.QueueTargetAgent, ID: "u"},
	} {
		p := resolve(t, nil, errors.New("db down"), target)
		if p.Enabled {
			t.Fatalf("config error must fail closed for %s, got enabled", target.Kind)
		}
	}
}
