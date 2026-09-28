package stage_repository

import (
	"testing"

	"vozko/domain/shared"
)

func TestEveryBoardChannelListsItsWorkspaceEntries(t *testing.T) {
	for _, e := range shared.CRMTaggableEntryTypes() {
		if _, ok := workspaceEntryIDSubqueries[e]; !ok {
			t.Errorf("%s has no workspace entry subquery; its cards would never reach the board", e)
		}
	}
}
