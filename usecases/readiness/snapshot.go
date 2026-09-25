package readiness_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/readiness"
	"vozko/domain/workspace/workspace_plan"
)

var ErrWorkspaceRequired = errors.New("readiness: workspace is required")

type SnapshotDeps struct {
	Subscription workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	Balance      balance.BalanceReader
	Probes       []readiness.Probe
}

type snapshotUseCase struct{ deps SnapshotDeps }

func NewSnapshotUseCase(deps SnapshotDeps) readiness.SnapshotUseCase {
	return &snapshotUseCase{deps: deps}
}

func (uc *snapshotUseCase) Snapshot(ctx context.Context, person readiness.Person) (*readiness.Snapshot, error) {
	if strings.TrimSpace(person.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	snap := &readiness.Snapshot{Capabilities: make([]readiness.Status, 0, len(uc.deps.Probes))}
	if uc.deps.Subscription != nil {
		_, err := uc.deps.Subscription.Execute(person.WorkspaceID)
		snap.SubscriptionActive = err == nil
	}
	if uc.deps.Balance != nil {
		if micros, err := uc.deps.Balance.GetBalance(person.WorkspaceID); err == nil {
			snap.BalanceMicros = micros
		}
	}
	for _, probe := range uc.deps.Probes {
		status, err := probe.Probe(ctx, person)
		if err != nil {
			status = readiness.Unavailable(probe.Capability())
		}
		snap.Capabilities = append(snap.Capabilities, status)
	}
	return snap, nil
}
