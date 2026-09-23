package attendance_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/attendance"
	at "vozko/domain/attendance_target"
)

func targetsServiceAt(t *testing.T, repo at.Repository, now string) *TargetsService {
	t.Helper()
	resolver := NewScheduleResolver(stubConfigReader{config: businessConfig()}, stubDepartmentSchedules{})
	svc := NewTargetsService(repo, resolver)
	parsed, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", now, err)
	}
	svc.SetClock(func() time.Time { return parsed })
	return svc
}

func septemberInput() UpsertTargetInput {
	return UpsertTargetInput{
		Scope:     at.ScopeWorkspace,
		MetricKey: attendance.MetricFinished,
		Period:    at.Month{Year: 2026, Month: time.September},
		Value:     1786,
	}
}

func TestTargetsServiceWithoutARepositoryIsUnavailable(t *testing.T) {
	svc := NewTargetsService(nil, nil)

	if _, err := svc.List(context.Background(), "ws1", at.Month{}, TargetAccess{}); !errors.Is(err, ErrTargetsUnavailable) {
		t.Fatalf("List() err = %v, want %v rather than an empty no-target list", err, ErrTargetsUnavailable)
	}
	if _, err := svc.Upsert(context.Background(), "ws1", septemberInput(), TargetAccess{}); !errors.Is(err, ErrTargetsUnavailable) {
		t.Fatalf("Upsert() err = %v, want %v", err, ErrTargetsUnavailable)
	}
	if err := svc.Delete(context.Background(), "ws1", "id", TargetAccess{}); !errors.Is(err, ErrTargetsUnavailable) {
		t.Fatalf("Delete() err = %v, want %v", err, ErrTargetsUnavailable)
	}
}

func TestTargetsServiceRefusesAClosedPeriod(t *testing.T) {
	svc := targetsServiceAt(t, &stubTargetRepo{}, "2026-10-05T12:00:00Z")

	_, err := svc.Upsert(context.Background(), "ws1", septemberInput(), TargetAccess{IsAdmin: true})
	if !errors.Is(err, at.ErrPeriodClosed) {
		t.Fatalf("Upsert() into a closed period err = %v, want %v", err, at.ErrPeriodClosed)
	}
}

func TestTargetsServiceAcceptsTheOpenPeriod(t *testing.T) {
	svc := targetsServiceAt(t, &stubTargetRepo{}, "2026-09-28T15:36:00Z")

	stored, err := svc.Upsert(context.Background(), "ws1", septemberInput(), TargetAccess{IsAdmin: true, UserID: "u1"})
	if err != nil {
		t.Fatalf("Upsert() err = %v, want nil", err)
	}
	if stored.PeriodStart.Day() != 1 || stored.PeriodStart.Month() != time.September {
		t.Fatalf("Upsert() PeriodStart = %v, want the first of September", stored.PeriodStart)
	}
	if stored.CreatedBy != "u1" {
		t.Fatalf("Upsert() CreatedBy = %q, want u1", stored.CreatedBy)
	}
}

func TestTargetsServiceRestrictedMemberCannotWriteWorkspaceTargets(t *testing.T) {
	svc := targetsServiceAt(t, &stubTargetRepo{}, "2026-09-28T15:36:00Z")
	access := TargetAccess{UserID: "u1", Restrict: true, DepartmentIDs: []string{"dept1"}}

	if _, err := svc.Upsert(context.Background(), "ws1", septemberInput(), access); !errors.Is(err, ErrTargetForbidden) {
		t.Fatalf("Upsert() of a workspace target by a restricted member err = %v, want %v", err, ErrTargetForbidden)
	}

	ownDepartment := septemberInput()
	ownDepartment.Scope = at.ScopeDepartment
	ownDepartment.ScopeID = "dept1"
	if _, err := svc.Upsert(context.Background(), "ws1", ownDepartment, access); err != nil {
		t.Fatalf("Upsert() of their own department target err = %v, want nil", err)
	}

	otherDepartment := septemberInput()
	otherDepartment.Scope = at.ScopeDepartment
	otherDepartment.ScopeID = "dept2"
	if _, err := svc.Upsert(context.Background(), "ws1", otherDepartment, access); !errors.Is(err, ErrTargetForbidden) {
		t.Fatalf("Upsert() into another department err = %v, want %v", err, ErrTargetForbidden)
	}
}

func TestTargetsServiceListFiltersByDepartmentScope(t *testing.T) {
	repo := &stubTargetRepo{targets: []at.Target{
		{Scope: at.ScopeWorkspace, MetricKey: attendance.MetricFinished, Value: 1000},
		{Scope: at.ScopeDepartment, ScopeID: "dept1", MetricKey: attendance.MetricFinished, Value: 400},
		{Scope: at.ScopeDepartment, ScopeID: "dept2", MetricKey: attendance.MetricFinished, Value: 600},
	}}
	svc := targetsServiceAt(t, repo, "2026-09-28T15:36:00Z")

	got, err := svc.List(context.Background(), "ws1", at.Month{}, TargetAccess{
		Restrict:      true,
		DepartmentIDs: []string{"dept1"},
	})
	if err != nil {
		t.Fatalf("List() err = %v, want nil", err)
	}
	for _, target := range got {
		if target.Scope == at.ScopeDepartment && target.ScopeID != "dept1" {
			t.Fatalf("List() leaked the %q target to a member scoped to dept1", target.ScopeID)
		}
	}
	if len(got) != 2 {
		t.Fatalf("List() returned %d targets, want 2", len(got))
	}
}

func TestTargetsServiceValidationBubblesUp(t *testing.T) {
	svc := targetsServiceAt(t, &stubTargetRepo{}, "2026-09-28T15:36:00Z")

	unknown := septemberInput()
	unknown.MetricKey = "invented"
	if _, err := svc.Upsert(context.Background(), "ws1", unknown, TargetAccess{IsAdmin: true}); !errors.Is(err, at.ErrUnknownMetric) {
		t.Fatalf("Upsert() err = %v, want %v", err, at.ErrUnknownMetric)
	}

	negative := septemberInput()
	negative.Value = -5
	if _, err := svc.Upsert(context.Background(), "ws1", negative, TargetAccess{IsAdmin: true}); !errors.Is(err, at.ErrInvalidValue) {
		t.Fatalf("Upsert() err = %v, want %v", err, at.ErrInvalidValue)
	}
}

func TestUpsertAcceptsTheMonthThatIsStillRunning(t *testing.T) {
	repo := &stubTargetRepo{}
	svc := targetsServiceAt(t, repo, "2026-09-23T12:00:00Z")

	saved, err := svc.Upsert(context.Background(), "ws1", septemberInput(), TargetAccess{})
	if err != nil {
		t.Fatalf("saving a goal for the month in progress failed: %v", err)
	}
	if saved == nil {
		t.Fatal("no target came back")
	}
	if saved.PeriodStart.Month() != time.September || saved.PeriodStart.Year() != 2026 {
		t.Fatalf("stored period = %s, want September 2026 in the workspace zone", saved.PeriodStart)
	}
	if saved.PeriodStart.Day() != 1 {
		t.Fatalf("stored period = %s, want the first of the month", saved.PeriodStart)
	}
}

func TestUpsertStillRefusesAMonthThatEnded(t *testing.T) {
	repo := &stubTargetRepo{}
	svc := targetsServiceAt(t, repo, "2026-11-02T12:00:00Z")

	if _, err := svc.Upsert(context.Background(), "ws1", septemberInput(), TargetAccess{}); !errors.Is(err, at.ErrPeriodClosed) {
		t.Fatalf("err = %v, want ErrPeriodClosed for a month that already ended", err)
	}
}

func TestListDefaultsToTheMonthInTheWorkspaceZone(t *testing.T) {
	repo := &stubTargetRepo{}
	svc := targetsServiceAt(t, repo, "2026-09-23T12:00:00Z")

	if _, err := svc.List(context.Background(), "ws1", at.Month{}, TargetAccess{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.listedPeriod.Month() != time.September || repo.listedPeriod.Year() != 2026 {
		t.Fatalf("listed %s, want September 2026", repo.listedPeriod)
	}
}
