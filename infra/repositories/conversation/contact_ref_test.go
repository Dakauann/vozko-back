package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

// THE property this file exists for: one expression, two shapes.
//
// The contact slot used to be declared twice — once in the workspace-wide
// registry, once in the entry-scoped one — in registries that named it
// differently and could not see each other. The only difference between the two
// spellings was a cast, and that is precisely how they drifted: the list showed
// the name an operator had typed and the conversation opened from it showed the
// provider's, because one of the two had been repointed and the other had not.
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

// The uuid shape must stay uncast. Every workspace-wide branch is UNIONed
// together and then joined to `leads` on a uuid column: one text cast makes
// Postgres resolve the whole union to text, and `l.id = ae.lead_id` fails
// outright rather than merely returning nothing.
func TestContactRef_UUIDShapeIsNeverCastToText(t *testing.T) {
	for entryType := range contactRefs {
		if strings.Contains(contactRefUUID(entryType), "::text") {
			t.Errorf("%s: uuid shape casts to text; the UNION and the leads join "+
				"both need uuid: %s", entryType, contactRefUUID(entryType))
		}
	}
}

// Every channel the entry registry knows about must declare a contact slot.
// An unknown channel falls back to NULL, which renders as "no contact" — safe,
// but silent, so the registries are checked against each other here instead.
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

// This channel's contacts ARE leads — unlike Instagram's and Telegram's, whose
// opaque provider ids have no lead to point at — so projecting the raw contact
// id made the leads join match nothing. The name came back empty, the inbox
// fell back to the handset's pushname, and renaming from the CRM answered
// "Lead not found" because the id in the request was a contact.
func TestContactRef_UnofficialWhatsAppResolvesTheLinkedLead(t *testing.T) {
	ref := contactRefUUID(shared.EntryTypeUnofficialWhatsApp)

	if !strings.Contains(ref, "unofficial_whatsapp_contacts") ||
		!strings.Contains(ref, "lead_id") {
		t.Fatalf("contact slot does not resolve the contact's lead: %s", ref)
	}

	// The fallback is load-bearing in the other direction: the identity
	// hydration looks the contact up by whatever is in this slot, and a group
	// (never linked to a lead) or a contact whose first message has not resolved
	// yet would otherwise render with no name, no avatar and no group flag.
	if !strings.Contains(ref, "COALESCE") || !strings.Contains(ref, "uwc.contact_id") {
		t.Fatalf("contact slot has no fallback for groups and unlinked contacts: %s", ref)
	}
}

// The channels whose contacts are genuinely not leads keep projecting their own
// contact id. There is no lead row to find, and inventing a join here would
// cost a query per page for nothing.
func TestContactRef_ChannelsWithoutLeadsProjectTheirContactID(t *testing.T) {
	for entryType, want := range map[shared.EntryType]string{
		shared.EntryTypeInstagram: "igc.contact_id",
		shared.EntryTypeTelegram:  "tgc.contact_id",
		// Official WhatsApp stores a real lead on the entry itself.
		shared.EntryTypeWhatsApp: "wce.lead_id",
	} {
		if got := contactRefUUID(entryType); got != want {
			t.Errorf("%s contact slot = %q, want %q", entryType, got, want)
		}
	}
}
