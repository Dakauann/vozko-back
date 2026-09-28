package workflow_repository

import (
	"testing"

	"vozko/domain/shared"
)

func TestEveryBoardChannelHasAnOwnershipQuery(t *testing.T) {
	for _, e := range shared.CRMTaggableEntryTypes() {
		if _, ok := ownershipQueries[e]; !ok {
			t.Errorf("%s has no ownership query; workflows would refuse every entry of it", e)
		}
	}
}
