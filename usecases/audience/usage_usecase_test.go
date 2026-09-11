package audience_usecase

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
)

// The budget screen has to answer two questions, and the second one is the
// reason the first is worth showing: what the ceiling is, and whether it is
// currently costing anything. A ceiling nobody is bumping into is trivia.

// fakeWorkspaceLimits is the workspace's own analysis settings, in memory.
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

// accountCap stands for a workspace configured before the workspace-level
// ceiling existed: its number lives on a channel account and nowhere else.
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
	// Another workspace's queue is not this workspace's problem.
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

// A backlog bigger than the room left is the state the operator needs to see,
// because it is the only one where raising the limit changes anything.
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

// A backlog read that fails must not take the whole panel down with it. The
// ceiling and the spend are still worth showing, and a missing backlog degrades
// to "nothing known to be waiting" rather than to an error page.
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

// The ceiling an operator sets has to be the ceiling the dashboard reports, and
// it has to beat whatever a channel account was carrying: the workspace screen
// is the control, so a stale account number must not quietly win.
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

// A workspace that analyses only conversations has no channel account at all.
// It was the case the old per-account ceiling could not express, so it ran under
// a number nobody could see or change.
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
