package inbox_assignment_usecase

import (
	"log"
	"time"

	wh "vozko/domain/working_hours"
	wsc "vozko/domain/workspace_config"
)

// tickSchedules holds every working-hours answer one sweep needs, compiled once.
//
// Two reads back it: the policy list the sweep already runs (which carries each
// workspace's schedule), and one query for the department overrides across all
// eligible workspaces. Nothing here is fetched per conversation — a tick with a
// full batch of 200 stalled conversations resolves exactly the same schedules as
// a tick with one.
type tickSchedules struct {
	workspaces  map[string]*wh.Schedule
	departments map[string]*wh.Schedule
	// workspaceOfDepartment lets a department override count towards its own
	// workspace when deciding whether that workspace can have work right now.
	departmentsOfWorkspace map[string][]string
}

// resolveSchedules compiles the workspace policies and reads the department
// overrides for this tick.
//
// A schedule that fails to compile is dropped, not treated as closed. Always
// open is the behaviour that predates working hours, so a bad policy costs a
// workspace its schedule rather than freezing every deadline inside it — the
// same posture the candidate resolver takes when presence reads fail.
func (j *RescueJob) resolveSchedules(policies []wsc.RoulettePolicy) *tickSchedules {
	s := &tickSchedules{
		workspaces:             make(map[string]*wh.Schedule, len(policies)),
		departments:            map[string]*wh.Schedule{},
		departmentsOfWorkspace: map[string][]string{},
	}

	workspaceIDs := make([]string, 0, len(policies))
	for _, p := range policies {
		workspaceIDs = append(workspaceIDs, p.WorkspaceID)
		if p.WorkingHours == nil {
			continue
		}
		compiled, err := p.WorkingHours.Compile()
		if err != nil {
			log.Printf("[assignment_rescue] workspace %s has an invalid working-hours policy; treating it as always open: %v", p.WorkspaceID, err)
			continue
		}
		s.workspaces[p.WorkspaceID] = compiled
	}

	if j.departments == nil || len(workspaceIDs) == 0 {
		return s
	}
	overrides, err := j.departments.ListWorkingHours(workspaceIDs)
	if err != nil {
		log.Printf("[assignment_rescue] department working-hours read failed; departments inherit their workspace this tick: %v", err)
		return s
	}
	for _, o := range overrides {
		compiled, err := o.WorkingHours.Compile()
		if err != nil {
			log.Printf("[assignment_rescue] department %s has an invalid working-hours policy; it inherits the workspace: %v", o.DepartmentID, err)
			continue
		}
		s.departments[o.DepartmentID] = compiled
		s.departmentsOfWorkspace[o.WorkspaceID] = append(s.departmentsOfWorkspace[o.WorkspaceID], o.DepartmentID)
	}
	return s
}

// forEntry is the schedule that governs one conversation: the department's own
// hours when it has them, otherwise the workspace's.
func (s *tickSchedules) forEntry(workspaceID, departmentID string) *wh.Schedule {
	if s == nil {
		return nil
	}
	var dept *wh.Schedule
	if departmentID != "" {
		dept = s.departments[departmentID]
	}
	return wh.Resolve(s.workspaces[workspaceID], dept)
}

// workspaceCanHaveWorkNow reports whether anything inside this workspace could
// be due right now.
//
// A workspace whose own hours are closed still qualifies when one of its
// departments keeps its own, currently-open schedule — the support desk working
// a Saturday inside a Mon-Fri company. Getting this wrong in the other
// direction would silently stop rescuing for that desk.
func (s *tickSchedules) workspaceCanHaveWorkNow(workspaceID string, now time.Time) bool {
	if s == nil {
		return true
	}
	if s.workspaces[workspaceID].IsOpen(now) {
		return true
	}
	for _, departmentID := range s.departmentsOfWorkspace[workspaceID] {
		if s.departments[departmentID].IsOpen(now) {
			return true
		}
	}
	return false
}

// reopenHint names when the next closed workspace opens again, so a quiet night
// in the log reads as "waiting until 09:00" instead of as a job that stopped.
func (s *tickSchedules) reopenHint(policies []wsc.RoulettePolicy, now time.Time) string {
	if s == nil {
		return ""
	}
	var soonest time.Time
	for _, p := range policies {
		if s.workspaceCanHaveWorkNow(p.WorkspaceID, now) {
			continue
		}
		next, ok := s.workspaces[p.WorkspaceID].NextOpen(now)
		if !ok {
			continue
		}
		if soonest.IsZero() || next.Before(soonest) {
			soonest = next
		}
	}
	if soonest.IsZero() {
		return ""
	}
	return "; next opening " + soonest.Format(time.RFC3339)
}
