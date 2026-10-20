package export

import (
	"testing"

	"vozko/domain/shared"
)

func TestExportableEntryTypesAreKnownConversationTypes(t *testing.T) {
	seen := map[EntryType]bool{}
	for _, entryType := range ExportableEntryTypes() {
		if seen[entryType] {
			t.Errorf("%s listed twice", entryType)
		}
		seen[entryType] = true
		if !shared.EntryType(entryType).IsKnown() {
			t.Errorf("%s is exportable but not a known entry type", entryType)
		}
	}
	for _, want := range []EntryType{EntryTypeWhatsApp, EntryTypeInstagram, EntryTypeTelegram, EntryTypeUnofficialWhatsApp, EntryTypeFacebook, EntryTypeWebchat, EntryTypeMercadoLivre} {
		if !seen[want] {
			t.Errorf("%s is missing from the exportable entry types", want)
		}
	}
}
