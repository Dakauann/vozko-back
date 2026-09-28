package workflow

import (
	"testing"

	"vozko/domain/shared"
)

func TestEveryChannelBranchHasAnOperatorLabel(t *testing.T) {
	for _, entryType := range ChannelBranchOrder {
		if ChannelBranchLabel(entryType) == string(entryType) {
			t.Errorf("%s has no operator-facing label", entryType)
		}
	}
	if ChannelBranchLabel(shared.EntryTypeFacebook) != "Messenger" {
		t.Errorf("facebook label = %q", ChannelBranchLabel(shared.EntryTypeFacebook))
	}
}
