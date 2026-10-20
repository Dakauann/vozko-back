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

func TestEveryMessagingChannelHasItsOwnBranch(t *testing.T) {
	inOrder := map[shared.EntryType]bool{}
	for _, entryType := range ChannelBranchOrder {
		inOrder[entryType] = true
	}
	for _, entryType := range shared.KnownEntryTypes() {
		if entryType.Valid() && !inOrder[entryType] {
			t.Errorf("%s reaches workflows but the channel node routes it to other channels", entryType)
		}
	}
	if ChannelBranchLabel(shared.EntryTypeMercadoLivre) != "Mercado Livre" {
		t.Errorf("mercadolivre label = %q", ChannelBranchLabel(shared.EntryTypeMercadoLivre))
	}
}
