package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	wd "vozko/domain/workspace/workspace_department"
	attendance_usecase "vozko/usecases/attendance"
)

type fakeMemberActivity struct {
	query attendance_usecase.MemberActivityQuery
	err   error
}

func (f *fakeMemberActivity) Execute(_ context.Context, q attendance_usecase.MemberActivityQuery) (*attendance_usecase.MemberActivityReport, error) {
	f.query = q
	if f.err != nil {
		return nil, f.err
	}
	report := &attendance_usecase.MemberActivityReport{MemberActivity: attendance.MemberActivity{
		Timezone: "America/Sao_Paulo",
		Days: []attendance.ActivityDay{
			{Date: "2026-09-21", ConnectedMS: 9 * 3600 * 1000, Sessions: []attendance.ActivitySession{{}}},
			{Date: "2026-09-22", Flags: []string{attendance.FlagNoPresence}},
		},
		Received: map[string]int{attendance.TriggerRoundRobin: 4},
	}}
	report.Heatmap[1][9] = 60
	return report, nil
}

const viewer = "9c1e2d3f-4a5b-4c6d-8e7f-0a1b2c3d4e5f"

func activityContext(view copilot.View) copilot.Context {
	return copilot.Context{
		WorkspaceID: "ws", UserID: viewer, Timezone: "America/Sao_Paulo", View: view,
		Departments: &wd.DepartmentFilter{DepartmentIDs: []string{knownDepartment}, WorkspaceHasDepartments: true},
		Datasets:    copilot.NewDatasetStore(),
	}
}

func TestMemberActivityAsksForTheMemberInTheViewersScope(t *testing.T) {
	reader := &fakeMemberActivity{}
	cc := activityContext(copilot.View{})
	res := NewMemberActivityTool(reader).Execute(context.Background(), cc, map[string]interface{}{"member_id": knownMember, "date_from": "2026-09-21", "date_to": "2026-09-22"})
	q := reader.query
	if res.Status != copilot.StatusOK || q.Self || q.MemberID != knownMember || q.Departments != cc.Departments || q.Timezone != "America/Sao_Paulo" || q.FromDay != "2026-09-21" {
		t.Fatalf("res %+v query %+v", res, q)
	}
	data := res.Data.(map[string]interface{})
	if data["days"] == nil || data["heatmap"] == nil {
		t.Fatalf("datasets missing: %+v", data)
	}
}

func TestMemberActivityDefaultsToTheScreenMemberThenToTheViewer(t *testing.T) {
	reader := &fakeMemberActivity{}
	NewMemberActivityTool(reader).Execute(context.Background(), activityContext(copilot.View{MemberID: knownMember}), nil)
	if reader.query.MemberID != knownMember || reader.query.Self {
		t.Fatalf("screen member: %+v", reader.query)
	}
	NewMemberActivityTool(reader).Execute(context.Background(), activityContext(copilot.View{}), nil)
	if reader.query.MemberID != viewer || !reader.query.Self {
		t.Fatalf("viewer: %+v", reader.query)
	}
}

func TestMemberActivityRefusesInventedOrOutOfScopeMembers(t *testing.T) {
	reader := &fakeMemberActivity{}
	if res := NewMemberActivityTool(reader).Execute(context.Background(), activityContext(copilot.View{}), map[string]interface{}{"member_id": "Marina"}); res.Status != copilot.StatusError || reader.query.MemberID != "" {
		t.Fatalf("invented id: %+v", res)
	}
	denied := NewMemberActivityTool(&fakeMemberActivity{err: attendance_usecase.ErrMemberOutOfScope}).Execute(context.Background(), activityContext(copilot.View{}), map[string]interface{}{"member_id": knownMember})
	if denied.Status != copilot.StatusDenied {
		t.Fatalf("out of scope: %+v", denied)
	}
	period := NewMemberActivityTool(&fakeMemberActivity{err: errors.Join(attendance_usecase.ErrActivityPeriod, errors.New("too long"))}).Execute(context.Background(), activityContext(copilot.View{}), nil)
	if period.Status != copilot.StatusError || period.Message == "" {
		t.Fatalf("period: %+v", period)
	}
}
