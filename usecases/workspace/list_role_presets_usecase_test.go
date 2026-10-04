package workspace_usecase

import (
	"testing"

	"vozko/domain/workspace"
)

func TestRolePresetsComeResolvedToPermissions(t *testing.T) {
	presets, err := NewListRolePresetsUseCase().Execute()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != len(workspace.RolePresets) {
		t.Fatalf("presets = %d, want %d", len(presets), len(workspace.RolePresets))
	}
	for _, p := range presets {
		if len(p.Permissions) == 0 {
			t.Errorf("preset %q resolved to no permissions", p.Key)
		}
	}
}
