package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestContactRef_BothShapesAreTheSameExpression(t *testing.T) {
	for entryType := range contactRefs {
		uuidForm := contactRefUUID(entryType)
		textForm := contactRefText(entryType)

		if !strings.Contains(textForm, uuidForm) {
			t.Errorf("%s: the text shape (%s) is not the uuid shape (%s) with a "+
				"cast — they have drifted apart", entryType, textForm, uuidForm)
		}
		if !strings.HasSuffix(textForm, "::text") {
			t.Errorf("%s: text shape does not cast: %s", entryType, textForm)
		}
	}
}

func TestContactRef_UUIDShapeIsNeverCastToText(t *testing.T) {
	for entryType := range contactRefs {
		if strings.Contains(contactRefUUID(entryType), "::text") {
			t.Errorf("%s: uuid shape casts to text; the UNION and the leads join "+
				"both need uuid: %s", entryType, contactRefUUID(entryType))
		}
	}
}

func TestContactRef_CoversEveryRegisteredChannel(t *testing.T) {
	for _, src := range entrySources {
		if _, ok := contactRefs[src.EntryType]; !ok {
			t.Errorf("%s is in entrySources but declares no contact slot; its "+
				"conversations would render with no contact at all", src.EntryType)
		}
	}
	for _, q := range channelQueries {
		if _, ok := contactRefs[q.EntryType]; !ok {
			t.Errorf("%s is in channelQueries but declares no contact slot", q.EntryType)
		}
	}
}

func TestContactRef_UnofficialWhatsAppResolvesTheLinkedLead(t *testing.T) {
	ref := contactRefUUID(shared.EntryTypeUnofficialWhatsApp)

	if !strings.Contains(ref, "unofficial_whatsapp_contacts") ||
		!strings.Contains(ref, "lead_id") {
		t.Fatalf("contact slot does not resolve the contact's lead: %s", ref)
	}

	if !strings.Contains(ref, "COALESCE") || !strings.Contains(ref, "uwc.contact_id") {
		t.Fatalf("contact slot has no fallback for groups and unlinked contacts: %s", ref)
	}
}

func TestContactRef_ChannelsWithoutLeadsProjectTheirContactID(t *testing.T) {
	for entryType, want := range map[shared.EntryType]string{
		shared.EntryTypeInstagram: "igc.contact_id",
		shared.EntryTypeTelegram:  "tgc.contact_id",
		shared.EntryTypeWhatsApp:  "wce.lead_id",
	} {
		if got := contactRefUUID(entryType); got != want {
			t.Errorf("%s contact slot = %q, want %q", entryType, got, want)
		}
	}
}
