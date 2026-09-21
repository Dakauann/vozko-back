package audience_usecase

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
)

func intp(n int) *int { return &n }

func TestWorkspaceSettings_UpdateLeavesUnsentFieldsAlone(t *testing.T) {
	store := newFakeWorkspaceLimits()
	uc := NewWorkspaceSettingsUseCase(store)
	ctx := context.Background()

	if _, err := uc.Update(ctx, "ws-1", ca.UpdateWorkspaceSettingsInput{DailyCap: intp(500)}); err != nil {
		t.Fatal(err)
	}
	got, err := uc.Update(ctx, "ws-1", ca.UpdateWorkspaceSettingsInput{DebounceMinutes: intp(30)})
	if err != nil {
		t.Fatal(err)
	}
	if got.DailyCap != 500 {
		t.Errorf("DailyCap = %d, want the earlier 500 preserved", got.DailyCap)
	}
	if got.DebounceMinutes != 30 {
		t.Errorf("DebounceMinutes = %d, want 30", got.DebounceMinutes)
	}
}

func TestWorkspaceSettings_UpdateRejectsOutOfRangeRatherThanClamping(t *testing.T) {
	uc := NewWorkspaceSettingsUseCase(newFakeWorkspaceLimits())
	ctx := context.Background()

	for _, bad := range []int{0, -5, ca.MaxDebounceMinutes + 1} {
		if _, err := uc.Update(ctx, "ws-1", ca.UpdateWorkspaceSettingsInput{DebounceMinutes: intp(bad)}); err == nil {
			t.Errorf("a debounce of %d was accepted", bad)
		}
	}
	if _, err := uc.Update(ctx, "ws-1", ca.UpdateWorkspaceSettingsInput{DailyCap: intp(0)}); err == nil {
		t.Error("a ceiling of zero was accepted")
	}
	if _, err := uc.Update(ctx, "ws-1", ca.UpdateWorkspaceSettingsInput{}); err == nil {
		t.Error("an empty update was accepted")
	}
	if _, err := uc.Update(ctx, "", ca.UpdateWorkspaceSettingsInput{DailyCap: intp(10)}); err == nil {
		t.Error("a write without a workspace was accepted")
	}
}

func TestWorkspaceSettings_ExecuteReportsStoredZeroesNotResolvedDefaults(t *testing.T) {
	uc := NewWorkspaceSettingsUseCase(newFakeWorkspaceLimits())

	got, err := uc.Execute(context.Background(), "ws-never-configured")
	if err != nil {
		t.Fatal(err)
	}
	if got.DebounceMinutes != 0 || got.DailyCap != 0 {
		t.Errorf("Execute = %+v, want zeroes for a workspace that never set anything", got)
	}
	if ca.ClampDebounceMinutes(got.DebounceMinutes) != ca.DefaultDebounceMinutes {
		t.Error("an unset debounce did not resolve to the default")
	}
}
