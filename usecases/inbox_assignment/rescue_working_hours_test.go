package inbox_assignment_usecase

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
	wh "vozko/domain/working_hours"
	wd "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

var saoPaulo = mustLoadSaoPaulo()

func mustLoadSaoPaulo() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic("working hours tests need tzdata: " + err.Error())
	}
	return loc
}

func officeSpec() *wh.Spec {
	weekday := []wh.Window{{Start: "09:00", End: "18:00"}}
	return &wh.Spec{
		Timezone: "America/Sao_Paulo",
		Days: map[string][]wh.Window{
			"mon": weekday, "tue": weekday, "wed": weekday,
			"thu": weekday, "fri": weekday,
		},
	}
}

func local(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, saoPaulo).UTC()
}

type stubDepartmentSchedules struct {
	rows  []wd.DepartmentSchedule
	err   error
	calls int
}

func (s *stubDepartmentSchedules) ListWorkingHours([]string) ([]wd.DepartmentSchedule, error) {
	s.calls++
	return s.rows, s.err
}

type hoursFixture struct {
	t     *testing.T
	base  *rescueFixture
	store *intervalStore
	job   *RescueJob
	depts *stubDepartmentSchedules
	now   time.Time
}

func newHoursFixture(t *testing.T, workspaceHours *wh.Spec, deptRows []wd.DepartmentSchedule) *hoursFixture {
	t.Helper()
	base := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})

	base.cfg.policies = []wsc.RoulettePolicy{{
		WorkspaceID:  "ws-1",
		RescueAfter:  15 * time.Minute,
		WorkingHours: workspaceHours,
	}}

	f := &hoursFixture{t: t, base: base, depts: &stubDepartmentSchedules{rows: deptRows}}
	f.store = newIntervalStore(func() time.Time { return f.now })
	base.svc.SetHistory(f.store)

	f.job = NewRescueJob(base.cfg, f.store, base.att, base.status, base.svc)
	f.job.SetDepartmentSchedules(f.depts)
	f.job.SetClock(func() time.Time { return f.now })
	return f
}

func (f *hoursFixture) handOutAt(at time.Time, owner, departmentID string) {
	f.now = at
	f.base.seedAssignment("entry-1", owner)
	require.NoError(f.t, f.store.Append(&ia.AssignmentHistory{
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       "whatsapp",
		AssignedActorID: owner,
		Trigger:         ia.TriggerInboundRR,
		BusinessPhoneID: "phone-1",
		DepartmentID:    departmentID,
	}))
}

func (f *hoursFixture) sweepAt(at time.Time) {
	f.t.Helper()
	f.now = at
	require.NoError(f.t, f.job.Execute(context.Background()))
}

func (f *hoursFixture) owner() string { return f.base.ownerOf("entry-1") }

func TestWorkingHours_DeadlineDoesNotRunOvernight(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), nil)
	f.handOutAt(local(2026, 9, 7, 17, 55), "ana", "")

	f.sweepAt(local(2026, 9, 7, 18, 10))
	assert.Equal(t, "ana", f.owner(), "ten wall-clock minutes later, but the office shut after five")

	f.sweepAt(local(2026, 9, 8, 3, 0))
	assert.Equal(t, "ana", f.owner(), "the middle of the night moves nothing")

	f.sweepAt(local(2026, 9, 8, 9, 5))
	assert.Equal(t, "ana", f.owner(), "ten minutes into the new day: fifteen not yet spent")

	f.sweepAt(local(2026, 9, 8, 9, 11))
	assert.Equal(t, "bob", f.owner(), "the deadline lands at 09:10, not at 18:10 the night before")
}

func TestWorkingHours_DeadlineSkipsTheWeekend(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), nil)
	f.handOutAt(local(2026, 9, 11, 17, 55), "ana", "")

	f.sweepAt(local(2026, 9, 12, 12, 0))
	assert.Equal(t, "ana", f.owner(), "saturday")
	f.sweepAt(local(2026, 9, 13, 12, 0))
	assert.Equal(t, "ana", f.owner(), "sunday")

	f.sweepAt(local(2026, 9, 14, 9, 11))
	assert.Equal(t, "bob", f.owner(), "monday morning")
}

func TestWorkingHours_InsideHoursBehavesAsBefore(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), nil)
	f.handOutAt(local(2026, 9, 7, 10, 0), "ana", "")

	f.sweepAt(local(2026, 9, 7, 10, 20))
	assert.Equal(t, "bob", f.owner())
}

func TestWorkingHours_UnconfiguredWorkspaceIsAlwaysOpen(t *testing.T) {
	f := newHoursFixture(t, nil, nil)
	f.handOutAt(local(2026, 9, 13, 2, 0), "ana", "")

	f.sweepAt(local(2026, 9, 13, 2, 20))
	assert.Equal(t, "bob", f.owner())
}

func saturdayDeskSpec() *wh.Spec {
	return &wh.Spec{
		Timezone: "America/Sao_Paulo",
		Days:     map[string][]wh.Window{"sat": {{Start: "08:00", End: "20:00"}}},
	}
}

func TestWorkingHours_DepartmentOverridesAClosedWorkspace(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), []wd.DepartmentSchedule{
		{DepartmentID: "dept-support", WorkspaceID: "ws-1", WorkingHours: saturdayDeskSpec()},
	})
	f.handOutAt(local(2026, 9, 12, 10, 0), "ana", "dept-support")

	f.sweepAt(local(2026, 9, 12, 10, 20))
	assert.Equal(t, "bob", f.owner(), "the workspace is shut but this desk is not")
}

func TestWorkingHours_DepartmentClosedInsideAnOpenWorkspace(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), []wd.DepartmentSchedule{
		{DepartmentID: "dept-night", WorkspaceID: "ws-1", WorkingHours: saturdayDeskSpec()},
	})
	f.handOutAt(local(2026, 9, 7, 10, 0), "ana", "dept-night")

	f.sweepAt(local(2026, 9, 7, 10, 30))
	assert.Equal(t, "ana", f.owner(), "workspace open, this department is not")
}

func TestWorkingHours_DepartmentWithoutOverrideInherits(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), []wd.DepartmentSchedule{
		{DepartmentID: "dept-other", WorkspaceID: "ws-1", WorkingHours: saturdayDeskSpec()},
	})
	f.handOutAt(local(2026, 9, 7, 17, 55), "ana", "dept-plain")

	f.sweepAt(local(2026, 9, 7, 18, 10))
	assert.Equal(t, "ana", f.owner(), "inherits the closed workspace")
	f.sweepAt(local(2026, 9, 8, 9, 11))
	assert.Equal(t, "bob", f.owner(), "and its reopening")
}

func TestWorkingHours_DepartmentSchedulesAreReadOncePerTick(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), []wd.DepartmentSchedule{
		{DepartmentID: "dept-support", WorkspaceID: "ws-1", WorkingHours: saturdayDeskSpec()},
	})
	f.handOutAt(local(2026, 9, 7, 10, 0), "ana", "dept-support")

	f.sweepAt(local(2026, 9, 7, 10, 30))
	assert.Equal(t, 1, f.depts.calls, "one read for the whole sweep")
}

func TestWorkingHours_ClosedWorkspaceSkipsTheCandidateQuery(t *testing.T) {
	base := newRescueFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)})
	base.cfg.policies = []wsc.RoulettePolicy{{
		WorkspaceID:  "ws-1",
		RescueAfter:  15 * time.Minute,
		WorkingHours: officeSpec(),
	}}
	job := NewRescueJob(base.cfg, base.history, base.att, base.status, base.svc)
	job.SetClock(func() time.Time { return local(2026, 9, 13, 3, 0) })

	require.NoError(t, job.Execute(context.Background()))
	assert.Nil(t, base.history.listArgs.workspaceIDs,
		"nothing can be due in a closed workspace, so nothing is queried for it")
}

func TestWorkingHours_InvalidPolicyDegradesToAlwaysOpen(t *testing.T) {
	broken := &wh.Spec{
		Timezone: "Mars/Olympus_Mons",
		Days:     map[string][]wh.Window{"mon": {{Start: "09:00", End: "18:00"}}},
	}
	f := newHoursFixture(t, broken, nil)
	f.handOutAt(local(2026, 9, 13, 2, 0), "ana", "")

	f.sweepAt(local(2026, 9, 13, 2, 20))
	assert.Equal(t, "bob", f.owner(), "a broken schedule must not stop the rescue")
}

func TestWorkingHours_DepartmentReadFailureFallsBackToInheriting(t *testing.T) {
	f := newHoursFixture(t, nil, nil)
	f.depts.err = assert.AnError
	f.handOutAt(local(2026, 9, 13, 2, 0), "ana", "dept-support")

	f.sweepAt(local(2026, 9, 13, 2, 20))
	assert.Equal(t, "bob", f.owner(), "inherits the workspace, which has no hours, so it is open")
}

func TestWorkingHours_ChainAdvancesOneHopPerWorkingDeadline(t *testing.T) {
	f := newHoursFixture(t, officeSpec(), nil)
	f.handOutAt(local(2026, 9, 7, 17, 55), "ana", "")

	f.sweepAt(local(2026, 9, 8, 9, 11))
	require.Equal(t, "bob", f.owner(), "hop 1 on tuesday morning")

	f.sweepAt(local(2026, 9, 8, 9, 20))
	assert.Equal(t, "bob", f.owner(), "bob's own deadline has not run yet")

	f.sweepAt(local(2026, 9, 8, 9, 27))
	assert.Equal(t, "cid", f.owner(), "hop 2, fifteen working minutes after hop 1")

	f.sweepAt(local(2026, 9, 8, 9, 43))
	assert.Equal(t, "ana", f.owner(), "hop 3 completes the lap")

	f.sweepAt(local(2026, 9, 8, 9, 59))
	assert.Equal(t, "", f.owner(), "ring walked, released to the department")
}
