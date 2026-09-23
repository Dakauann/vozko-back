package attendance_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/attendance"
	wh "vozko/domain/working_hours"
	dept "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

var ErrScheduleInvalid = errors.New("attendance: the working-hours schedule stored for this scope is invalid")

const ReasonInvalidSchedule = "invalid_schedule"

type WorkspaceConfigReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type DepartmentScheduleReader interface {
	ListWorkingHours(workspaceIDs []string) ([]dept.DepartmentSchedule, error)
}

type ScheduleResolver struct {
	configs     WorkspaceConfigReader
	departments DepartmentScheduleReader
}

func NewScheduleResolver(configs WorkspaceConfigReader, departments DepartmentScheduleReader) *ScheduleResolver {
	return &ScheduleResolver{configs: configs, departments: departments}
}

func (r *ScheduleResolver) Config(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error) {
	if r == nil || r.configs == nil || strings.TrimSpace(workspaceID) == "" {
		return nil, nil
	}
	return r.configs.GetByWorkspaceID(ctx, workspaceID)
}

func (r *ScheduleResolver) Resolve(ctx context.Context, workspaceID, departmentID string) (*wh.Schedule, error) {
	config, err := r.Config(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return r.ResolveFromConfig(config, workspaceID, departmentID)
}

func (r *ScheduleResolver) ResolveFromConfig(config *wsc.WorkspaceConfig, workspaceID, departmentID string) (*wh.Schedule, error) {
	var workspaceSchedule *wh.Schedule
	if config != nil && config.WorkingHours != nil {
		compiled, err := config.WorkingHours.Compile()
		if err != nil {
			return nil, ErrScheduleInvalid
		}
		workspaceSchedule = compiled
	}

	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" || r == nil || r.departments == nil {
		return workspaceSchedule, nil
	}

	overrides, err := r.departments.ListWorkingHours([]string{workspaceID})
	if err != nil {
		return nil, err
	}
	for _, override := range overrides {
		if override.DepartmentID != departmentID || override.WorkingHours == nil {
			continue
		}
		compiled, err := override.WorkingHours.Compile()
		if err != nil {
			return nil, ErrScheduleInvalid
		}
		return wh.Resolve(workspaceSchedule, compiled), nil
	}
	return workspaceSchedule, nil
}

func periodFor(sched *wh.Schedule, filter attendance.OverviewFilter, now time.Time) (time.Time, time.Time) {
	anchor := now
	if filter.DateTo != nil {
		anchor = *filter.DateTo
	}
	var loc *time.Location
	if sched != nil {
		loc = sched.Location()
	}
	return attendance.MonthRange(anchor, loc)
}

func scheduleLocation(sched *wh.Schedule) *time.Location {
	if sched == nil {
		return time.UTC
	}
	return sched.Location()
}
