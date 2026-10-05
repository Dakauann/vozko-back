package attendance_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ap "vozko/domain/agent_presence"
	"vozko/domain/attendance"
	ia "vozko/domain/inbox_assignment"
	wh "vozko/domain/working_hours"
	dept "vozko/domain/workspace/workspace_department"
)

const (
	MaxActivityDays     = 92
	DefaultActivityDays = 7
)

var (
	ErrMemberOutOfScope = errors.New("attendance: this member is outside your departments")
	ErrActivityPeriod   = errors.New("attendance: the activity period must be valid days, up to 92, in a known timezone")
)

type PresenceSpanReader interface {
	Spans(workspaceID, userID string, from, to time.Time) ([]ap.Interval, error)
}

type ActorHistoryReader interface {
	ListByActor(workspaceID, actorID string, from, to time.Time) ([]*ia.AssignmentHistory, error)
}

type MemberDepartmentReader interface {
	GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error)
}

type TeamSectionReader interface {
	Team(ctx context.Context, workspaceID string, filter attendance.OverviewFilter) (*attendance.TeamSection, error)
}

type ScheduleSource interface {
	Resolve(ctx context.Context, workspaceID, departmentID string) (*wh.Schedule, error)
}

type MemberActivityDeps struct {
	Presence    PresenceSpanReader
	History     ActorHistoryReader
	Departments MemberDepartmentReader
	Team        TeamSectionReader
	Schedules   ScheduleSource
	Now         func() time.Time
}

type MemberActivityQuery struct {
	WorkspaceID string
	MemberID    string
	FromDay     string
	ToDay       string
	Timezone    string
	Self        bool
	Departments *dept.DepartmentFilter
}

type MemberActivityReport struct {
	attendance.MemberActivity
	Work *attendance.MemberRow `json:"work,omitempty"`
}

type MemberActivityUseCase struct {
	deps MemberActivityDeps
}

func NewMemberActivityUseCase(deps MemberActivityDeps) *MemberActivityUseCase {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &MemberActivityUseCase{deps: deps}
}

func (uc *MemberActivityUseCase) Execute(ctx context.Context, q MemberActivityQuery) (*MemberActivityReport, error) {
	if err := uc.authorize(q); err != nil {
		return nil, err
	}
	loc, err := uc.location(ctx, q)
	if err != nil {
		return nil, err
	}
	now := uc.deps.Now()
	from, to, err := activityPeriod(q.FromDay, q.ToDay, now, loc)
	if err != nil {
		return nil, err
	}
	intervals, err := uc.deps.Presence.Spans(q.WorkspaceID, q.MemberID, from, to)
	if err != nil {
		return nil, err
	}
	history, err := uc.deps.History.ListByActor(q.WorkspaceID, q.MemberID, from, to)
	if err != nil {
		return nil, err
	}
	team, err := uc.deps.Team.Team(ctx, q.WorkspaceID, attendance.OverviewFilter{MemberID: q.MemberID, DateFrom: &from, DateTo: &to, IncludeAI: true})
	if err != nil {
		return nil, err
	}
	report := &MemberActivityReport{MemberActivity: attendance.BuildMemberActivity(attendance.ActivityInput{
		Spans: presenceSpans(intervals), Handouts: handouts(history), From: from, To: to, Now: now, Location: loc,
	})}
	for i := range team.ByMember {
		if team.ByMember[i].ActorID == q.MemberID {
			report.Work = &team.ByMember[i]
		}
	}
	return report, nil
}

func (uc *MemberActivityUseCase) authorize(q MemberActivityQuery) error {
	if q.Self {
		return nil
	}
	departments, err := uc.deps.Departments.GetMemberDepartmentIDs(q.WorkspaceID, q.MemberID)
	if err != nil {
		return err
	}
	if !q.Departments.AllowsMember(departments) {
		return ErrMemberOutOfScope
	}
	return nil
}

func (uc *MemberActivityUseCase) location(ctx context.Context, q MemberActivityQuery) (*time.Location, error) {
	if uc.deps.Schedules != nil {
		sched, err := uc.deps.Schedules.Resolve(ctx, q.WorkspaceID, "")
		if err != nil {
			return nil, err
		}
		if sched != nil {
			return sched.Location(), nil
		}
	}
	tz := strings.TrimSpace(q.Timezone)
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivityPeriod, err)
	}
	return loc, nil
}

func activityPeriod(fromDay, toDay string, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	today := now.In(loc)
	last := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	first := last.AddDate(0, 0, -(DefaultActivityDays - 1))
	var err error
	if strings.TrimSpace(toDay) != "" {
		if last, err = time.ParseInLocation(attendance.DayLayout, strings.TrimSpace(toDay), loc); err != nil {
			return time.Time{}, time.Time{}, ErrActivityPeriod
		}
		first = last.AddDate(0, 0, -(DefaultActivityDays - 1))
	}
	if strings.TrimSpace(fromDay) != "" {
		if first, err = time.ParseInLocation(attendance.DayLayout, strings.TrimSpace(fromDay), loc); err != nil {
			return time.Time{}, time.Time{}, ErrActivityPeriod
		}
	}
	days := int(last.Sub(first).Hours()/24) + 1
	if days < 1 || days > MaxActivityDays {
		return time.Time{}, time.Time{}, ErrActivityPeriod
	}
	return first.UTC(), last.AddDate(0, 0, 1).Add(-time.Second).UTC(), nil
}

func presenceSpans(intervals []ap.Interval) []attendance.PresenceSpan {
	out := make([]attendance.PresenceSpan, 0, len(intervals))
	for _, iv := range intervals {
		span := attendance.PresenceSpan{Start: iv.StartedAt, OnCall: iv.State == ap.StateOnCall, Open: iv.EndedAt == nil}
		if iv.EndedAt != nil {
			span.End = *iv.EndedAt
		}
		out = append(out, span)
	}
	return out
}

func handouts(history []*ia.AssignmentHistory) []attendance.Handout {
	out := make([]attendance.Handout, 0, len(history))
	for _, h := range history {
		out = append(out, attendance.Handout{At: h.StartedAt, Trigger: h.Trigger})
	}
	return out
}
