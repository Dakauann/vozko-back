package attendance_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	ap "vozko/domain/agent_presence"
	"vozko/domain/attendance"
	ia "vozko/domain/inbox_assignment"
	dept "vozko/domain/workspace/workspace_department"
)

type spansStub struct {
	spans []ap.Interval
	from  time.Time
	to    time.Time
}

func (s *spansStub) Spans(_, _ string, from, to time.Time) ([]ap.Interval, error) {
	s.from, s.to = from, to
	return s.spans, nil
}

type actorHistoryStub struct{ rows []*ia.AssignmentHistory }

func (s actorHistoryStub) ListByActor(string, string, time.Time, time.Time) ([]*ia.AssignmentHistory, error) {
	return s.rows, nil
}

type memberDepartmentsStub struct {
	ids []string
	err error
}

func (s memberDepartmentsStub) GetMemberDepartmentIDs(string, string) ([]string, error) {
	return s.ids, s.err
}

type teamStub struct {
	filter attendance.OverviewFilter
	err    error
}

func (s *teamStub) Team(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.TeamSection, error) {
	s.filter = filter
	if s.err != nil {
		return nil, s.err
	}
	return &attendance.TeamSection{ByMember: []attendance.MemberRow{{ActorID: "other", Resolved: 9}, {ActorID: "marina", Resolved: 42}}}, nil
}

func activityUseCase(team *teamStub, spans *spansStub, departments memberDepartmentsStub) *MemberActivityUseCase {
	return NewMemberActivityUseCase(MemberActivityDeps{
		Presence:    spans,
		History:     actorHistoryStub{rows: []*ia.AssignmentHistory{{Trigger: ia.TriggerInboundRR, StartedAt: time.Date(2026, 9, 22, 13, 0, 0, 0, time.UTC)}}},
		Departments: departments,
		Team:        team,
		Now:         func() time.Time { return time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC) },
	})
}

func restrictedTo(ids ...string) *dept.DepartmentFilter {
	return &dept.DepartmentFilter{DepartmentIDs: ids, WorkspaceHasDepartments: true}
}

func TestAManagerSeesAMemberOfTheirDepartment(t *testing.T) {
	team, spans := &teamStub{}, &spansStub{spans: []ap.Interval{{State: ap.StateOnline, StartedAt: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}}}
	report, err := activityUseCase(team, spans, memberDepartmentsStub{ids: []string{"vendas"}}).Execute(context.Background(), MemberActivityQuery{
		WorkspaceID: "ws", MemberID: "marina", FromDay: "2026-09-21", ToDay: "2026-09-27", Timezone: "America/Sao_Paulo",
		Departments: restrictedTo("vendas"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Timezone != "America/Sao_Paulo" || len(report.Days) != 7 || report.Received[ia.TriggerInboundRR] != 1 {
		t.Fatalf("report %+v", report.MemberActivity)
	}
	if report.Work == nil || report.Work.Resolved != 42 || team.filter.MemberID != "marina" {
		t.Fatalf("work %+v filter %+v", report.Work, team.filter)
	}
	if spans.from.Format(time.RFC3339) != "2026-09-21T03:00:00Z" {
		t.Fatalf("the period starts at local midnight, got %s", spans.from)
	}
}

func TestAMemberOutsideTheViewersDepartmentsIsRefused(t *testing.T) {
	cases := map[string]struct {
		filter      *dept.DepartmentFilter
		departments memberDepartmentsStub
		want        error
	}{
		"other department":       {restrictedTo("vendas"), memberDepartmentsStub{ids: []string{"suporte"}}, ErrMemberOutOfScope},
		"no viewer scope":        {nil, memberDepartmentsStub{ids: []string{"vendas"}}, ErrMemberOutOfScope},
		"departments unreadable": {restrictedTo("vendas"), memberDepartmentsStub{err: errors.New("db down")}, nil},
	}
	for name, tc := range cases {
		_, err := activityUseCase(&teamStub{}, &spansStub{}, tc.departments).Execute(context.Background(), MemberActivityQuery{
			WorkspaceID: "ws", MemberID: "marina", Departments: tc.filter,
		})
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func TestEveryoneSeesTheirOwnActivity(t *testing.T) {
	_, err := activityUseCase(&teamStub{}, &spansStub{}, memberDepartmentsStub{err: errors.New("never asked")}).Execute(context.Background(), MemberActivityQuery{
		WorkspaceID: "ws", MemberID: "marina", Self: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestThePeriodIsBoundedAndDefaultsToTheLastWeek(t *testing.T) {
	spans := &spansStub{}
	uc := activityUseCase(&teamStub{}, spans, memberDepartmentsStub{})
	report, err := uc.Execute(context.Background(), MemberActivityQuery{WorkspaceID: "ws", MemberID: "marina", Self: true})
	if err != nil || len(report.Days) != 7 || report.Timezone != "UTC" {
		t.Fatalf("report %+v err %v", report, err)
	}
	for _, q := range []MemberActivityQuery{
		{FromDay: "2026-01-01", ToDay: "2026-09-27"},
		{FromDay: "2026-09-27", ToDay: "2026-09-20"},
		{FromDay: "ontem", ToDay: "2026-09-27"},
		{Timezone: "Lua/Cratera"},
	} {
		q.WorkspaceID, q.MemberID, q.Self = "ws", "marina", true
		if _, err := uc.Execute(context.Background(), q); !errors.Is(err, ErrActivityPeriod) {
			t.Errorf("%+v: err %v", q, err)
		}
	}
}

func TestTheReportFailsWhenTheWorkNumbersFail(t *testing.T) {
	_, err := activityUseCase(&teamStub{err: errors.New("busy")}, &spansStub{}, memberDepartmentsStub{}).Execute(context.Background(), MemberActivityQuery{
		WorkspaceID: "ws", MemberID: "marina", Self: true,
	})
	if err == nil {
		t.Fatal("a partial report would hide the missing numbers")
	}
}
