package database

import (
	"slices"
	"testing"
)

func TestTheWorkspaceExitSettingsTableIsDroppedOnBoot(t *testing.T) {
	if !slices.Contains(repairNames(), "lx_drop_workspace_exit_settings") {
		t.Fatal("the workspace exit settings table is no longer used and must be dropped")
	}
}
