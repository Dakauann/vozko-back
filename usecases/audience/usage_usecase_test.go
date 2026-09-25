package audience_usecase

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
)

type fakeWorkspaceLimits struct {
	byWorkspace map[string]ca.WorkspaceSettings
}

func newFakeWorkspaceLimits() *fakeWorkspaceLimits {
	return &fakeWorkspaceLimits{byWorkspace: map[string]ca.WorkspaceSettings{}}
}

func (f *fakeWorkspaceLimits) Get(_ context.Context, workspaceID string) (ca.WorkspaceSettings, error) {
	return f.byWorkspace[workspaceID], nil
}

func (f *fakeWorkspaceLimits) Save(_ context.Context, workspaceID string, settings ca.WorkspaceSettings) error {
	f.byWorkspace[workspaceID] = settings
	return nil
}

func usageHarness(t *testing.T, accountCap int) (ca.UsageUseCase, *fakeRepo, *fakeWorkspaceLimits, *fakeSettings) {
	t.Helper()
	repo := newFakeRepo()
	settings := newFakeSettings(&ca.Settings{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", DailyCap: accountCap,
	})
	limits := newFakeWorkspaceLimits()
	uc := NewUsageUseCase(NewUsageLimiter(newFakeState()), limits, settings, repo, fixedClock{now})
	return uc, repo, limits, settings
}

func TestUsage_ReportsTheBacklogBehindTheCeiling(t *testing.T) {
	uc, repo, _, _ := usageHarness(t, 100)
	ctx := context.Background()

	repo.seedWaiting("ws-1", 12)
	repo.seedWaiting("ws-2", 40)

	u, err := uc.Execute(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.Waiting != 12 {
		t.Errorf("Waiting = %d, want 12", u.Waiting)
	}
	if u.Limit != 100 {
		t.Errorf("Limit = %d, want 100", u.Limit)
	}
	if u.Constrained() {
		t.Error("12 waiting against a 100 ceiling blamed the ceiling")
	}
}

func TestUsage_MarksTheCeilingAsTheConstraintWhenWorkIsStackingUp(t *testing.T) {
	uc, repo, _, _ := usageHarness(t, 10)
	ctx := context.Background()

	repo.seedWaiting("ws-1", 50)

	u, err := uc.Execute(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if !u.Constrained() {
		t.Fatalf("50 waiting against a ceiling of 10 was not reported as constrained: %+v", u)
	}
	if u.ClearsIn() <= 0 {
		t.Error("a constrained budget reported no wait to clear")
	}
}

func TestUsage_SurvivesABacklogReadFailure(t *testing.T) {
	uc, repo, _, _ := usageHarness(t, 100)
	repo.failCountWaiting = true

	u, err := uc.Execute(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("a backlog read failure took the budget down: %v", err)
	}
	if u.Limit != 100 {
		t.Errorf("Limit = %d, want the ceiling to survive", u.Limit)
	}
	if u.Waiting != 0 {
		t.Errorf("Waiting = %d, want 0 when it could not be read", u.Waiting)
	}
}

func TestUsage_WorkspaceCeilingBeatsTheAccountCeiling(t *testing.T) {
	uc, _, limits, _ := usageHarness(t, 20000)
	ctx := context.Background()

	before, err := uc.Execute(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if before.Limit != 20000 {
		t.Fatalf("Limit before = %d, want the account ceiling 20000", before.Limit)
	}

	if err := limits.Save(ctx, "ws-1", ca.WorkspaceSettings{DailyCap: 250}); err != nil {
		t.Fatal(err)
	}
	after, err := uc.Execute(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Limit != 250 {
		t.Errorf("Limit after = %d, want the workspace ceiling 250", after.Limit)
	}
}

func TestUsage_ReportsACeilingWithoutAnyChannelAccount(t *testing.T) {
	limits := newFakeWorkspaceLimits()
	uc := NewUsageUseCase(NewUsageLimiter(newFakeState()), limits, newFakeSettings(), newFakeRepo(), fixedClock{now})
	ctx := context.Background()

	if err := limits.Save(ctx, "ws-conversations-only", ca.WorkspaceSettings{DailyCap: 300}); err != nil {
		t.Fatal(err)
	}
	u, err := uc.Execute(ctx, "ws-conversations-only")
	if err != nil {
		t.Fatal(err)
	}
	if u.Limit != 300 {
		t.Errorf("Limit = %d, want 300", u.Limit)
	}
}

func TestUsage_LimitMatchesTheCeilingTheUsageReports(t *testing.T) {
	uc, repo, limits, _ := usageHarness(t, 20000)
	ctx := context.Background()
	repo.seedWaiting("ws-1", 7)

	for _, workspaceCap := range []int{0, 250} {
		if err := limits.Save(ctx, "ws-1", ca.WorkspaceSettings{DailyCap: workspaceCap}); err != nil {
			t.Fatal(err)
		}
		usage, err := uc.Execute(ctx, "ws-1")
		if err != nil {
			t.Fatal(err)
		}
		limit, err := uc.Limit(ctx, "ws-1")
		if err != nil {
			t.Fatal(err)
		}
		if limit != usage.Limit {
			t.Fatalf("Limit() = %d, Execute().Limit = %d with a workspace cap of %d", limit, usage.Limit, workspaceCap)
		}
	}
}

func TestUsage_LimitNeedsAWorkspace(t *testing.T) {
	uc, _, _, _ := usageHarness(t, 100)
	if _, err := uc.Limit(context.Background(), ""); err != ca.ErrWorkspaceRequired {
		t.Fatalf("Limit(\"\") error = %v, want ErrWorkspaceRequired", err)
	}
}
