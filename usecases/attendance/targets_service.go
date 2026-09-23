package attendance_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	at "vozko/domain/attendance_target"
)

var (
	ErrTargetsUnavailable = errors.New("attendance targets: the targets repository is not configured")
	ErrTargetForbidden    = errors.New("attendance targets: this scope is outside your departments")
)

type TargetAccess struct {
	UserID        string
	IsAdmin       bool
	DepartmentIDs []string
	Restrict      bool
}

func (a TargetAccess) restricted() bool {
	return a.Restrict && !a.IsAdmin
}

func (a TargetAccess) ownsDepartment(departmentID string) bool {
	for _, id := range a.DepartmentIDs {
		if id == departmentID {
			return true
		}
	}
	return false
}

func (a TargetAccess) mayWrite(scope at.Scope, scopeID string) bool {
	if !a.restricted() {
		return true
	}
	if scope != at.ScopeDepartment {
		return false
	}
	return a.ownsDepartment(scopeID)
}

type UpsertTargetInput struct {
	Scope     at.Scope
	ScopeID   string
	MetricKey string
	Period    at.Month
	Value     float64
	Currency  string
}

type TargetsService struct {
	repo      at.Repository
	schedules *ScheduleResolver
	now       func() time.Time
}

func NewTargetsService(repo at.Repository, schedules *ScheduleResolver) *TargetsService {
	return &TargetsService{repo: repo, schedules: schedules, now: func() time.Time { return time.Now().UTC() }}
}

func (s *TargetsService) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.now = clock
	}
}

func (s *TargetsService) ready() error {
	if s == nil || s.repo == nil {
		return ErrTargetsUnavailable
	}
	return nil
}

func (s *TargetsService) location(ctx context.Context, workspaceID string) (*time.Location, error) {
	if s.schedules == nil {
		return time.UTC, nil
	}
	sched, err := s.schedules.Resolve(ctx, workspaceID, "")
	if err != nil {
		if errors.Is(err, ErrScheduleInvalid) {
			return time.UTC, nil
		}
		return nil, err
	}
	return scheduleLocation(sched), nil
}

func (s *TargetsService) resolveMonth(month at.Month, loc *time.Location) time.Time {
	if month.IsZero() {
		month = at.MonthAt(s.now(), loc)
	}
	return month.Start(loc)
}

func (s *TargetsService) List(ctx context.Context, workspaceID string, month at.Month, access TargetAccess) ([]at.Target, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	loc, err := s.location(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	targets, err := s.repo.ListForPeriod(workspaceID, s.resolveMonth(month, loc))
	if err != nil {
		return nil, err
	}
	return at.FilterVisible(targets, access.DepartmentIDs, access.restricted()), nil
}

func (s *TargetsService) ListForOverview(ctx context.Context, workspaceID string, period time.Time, loc *time.Location) ([]at.Target, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.repo.ListForPeriod(workspaceID, at.NormalizePeriodStart(period, loc))
}

func (s *TargetsService) Upsert(ctx context.Context, workspaceID string, input UpsertTargetInput, access TargetAccess) (*at.Target, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	loc, err := s.location(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	target := at.Target{
		WorkspaceID: workspaceID,
		Scope:       input.Scope,
		ScopeID:     input.ScopeID,
		MetricKey:   input.MetricKey,
		PeriodStart: s.resolveMonth(input.Period, loc),
		Value:       input.Value,
		Currency:    input.Currency,
		CreatedBy:   strings.TrimSpace(access.UserID),
	}
	target.Normalize(loc)
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if !access.mayWrite(target.Scope, target.ScopeID) {
		return nil, ErrTargetForbidden
	}
	if target.PeriodHasClosed(s.now().In(loc)) {
		return nil, at.ErrPeriodClosed
	}
	return s.repo.Upsert(target)
}

func (s *TargetsService) Delete(ctx context.Context, workspaceID, id string, access TargetAccess) error {
	if err := s.ready(); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return err
	}
	if !access.mayWrite(existing.Scope, existing.ScopeID) {
		return ErrTargetForbidden
	}
	loc, err := s.location(ctx, workspaceID)
	if err != nil {
		return err
	}
	if existing.PeriodHasClosed(s.now().In(loc)) {
		return at.ErrPeriodClosed
	}
	return s.repo.Delete(workspaceID, id)
}
