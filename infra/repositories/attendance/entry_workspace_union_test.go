package attendance_repository

import (
	"strings"
	"testing"
)

func TestEntryWorkspaceUnionCoversEveryChannelSource(t *testing.T) {
	union := EntryWorkspaceUnion()
	selects := strings.Split(union, " UNION ALL ")
	if len(selects) != len(channelSources) {
		t.Fatalf("union has %d selects for %d channel sources", len(selects), len(channelSources))
	}
	for i, src := range channelSources {
		part := selects[i]
		for _, want := range []string{
			"FROM " + src.EntryTable,
			src.EntryAlias + ".id AS entry_id",
			"'" + string(src.EntryType) + "'::text AS entry_type",
			src.WorkspaceColumn + " AS workspace_id",
		} {
			if !strings.Contains(part, want) {
				t.Errorf("%s select %q lacks %q", src.EntryType, part, want)
			}
		}
		joinsContainer := strings.Contains(part, "JOIN "+src.ContainerTable+" ON "+src.ContainerJoin)
		ownColumn := strings.HasPrefix(src.WorkspaceColumn, src.EntryAlias+".")
		if joinsContainer == ownColumn {
			t.Errorf("%s select %q joins container = %v though workspace column is %s", src.EntryType, part, joinsContainer, src.WorkspaceColumn)
		}
	}
}

func TestEntryWorkspaceUnionOnlyReferencesRealColumns(t *testing.T) {
	assertColumnsExist(t, "entry workspace union", EntryWorkspaceUnion())
}
