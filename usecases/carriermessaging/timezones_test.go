package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/working_hours"
	wsc "vozko/domain/workspace_config"
)

type fakeConfigs struct {
	config *wsc.WorkspaceConfig
	err    error
}

func (f fakeConfigs) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return f.config, f.err
}

func TestWorkspaceTimezoneComesFromItsWorkingHours(t *testing.T) {
	tz, err := NewWorkspaceTimezones(fakeConfigs{config: &wsc.WorkspaceConfig{WorkingHours: &working_hours.Spec{Timezone: "America/Manaus"}}}).Timezone(context.Background(), "ws-1")
	if err != nil || tz != "America/Manaus" {
		t.Fatalf("Timezone = %q, %v", tz, err)
	}
	tz, _ = NewWorkspaceTimezones(fakeConfigs{}).Timezone(context.Background(), "ws-1")
	if tz != "America/Sao_Paulo" {
		t.Fatalf("default = %q", tz)
	}
	if _, err := NewWorkspaceTimezones(fakeConfigs{err: errors.New("down")}).Timezone(context.Background(), "ws-1"); err == nil {
		t.Fatal("a read failure must not fall back to a guess")
	}
}
