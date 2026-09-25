package attendance_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	wh "vozko/domain/working_hours"
	dept "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

type configStub struct {
	config *wsc.WorkspaceConfig
	err    error
}

func (s configStub) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return s.config, s.err
}

type noDepartmentSchedules struct{}

func (noDepartmentSchedules) ListWorkingHours([]string) ([]dept.DepartmentSchedule, error) {
	return nil, nil
}

func TestLocationIsTheZoneAttendanceCountsDaysIn(t *testing.T) {
	spec := &wh.Spec{Timezone: "America/Fortaleza", Days: map[string][]wh.Window{"mon": {{Start: "09:00", End: "18:00"}}}}
	resolver := NewScheduleResolver(configStub{config: &wsc.WorkspaceConfig{WorkingHours: spec}}, noDepartmentSchedules{})
	loc, err := resolver.Location(context.Background(), "ws1", "")
	if err != nil || loc.String() != "America/Fortaleza" {
		t.Fatalf("Location() = %v, %v", loc, err)
	}
}

func TestLocationWithoutAScheduleIsUTCLikeAttendance(t *testing.T) {
	loc, err := NewScheduleResolver(configStub{}, noDepartmentSchedules{}).Location(context.Background(), "ws1", "")
	// The attendance sections fall back to UTC for a workspace with no schedule; anything else would split days differently.
	if err != nil || loc != time.UTC {
		t.Fatalf("Location() = %v, %v, want UTC", loc, err)
	}
}

func TestLocationSurfacesAConfigFailure(t *testing.T) {
	boom := errors.New("db down")
	if _, err := NewScheduleResolver(configStub{err: boom}, noDepartmentSchedules{}).Location(context.Background(), "ws1", ""); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
