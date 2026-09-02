package conversation_repository

import "vozko/domain/shared"

// Who a conversation is WITH, declared once per channel.
//
// Every read path carries this value in a slot the rest of the system calls
// `lead_id`, and it is the id the UI then addresses for a rename, a block, a
// memory or an opportunity. It has to be the same expression everywhere, and it
// was not: the workspace-wide UNION and the entry-scoped projection each
// declared it, in registries with different field names, and the only
// difference between the two spellings was a `::text` cast.
//
// That is the shape of a bug waiting to happen, and it happened. Pointing the
// unofficial WhatsApp slot at the CRM lead it had always had needed the same
// edit in both, plus a third in the header query composed from one of them.
// Two of the three were found on the first pass; the list showed the operator's
// new name while the conversation they opened from it still showed the old one.
//
// One definition. Both shapes read it, and each adds the cast its own query
// wants — the UNION needs uuid so its branches agree and the `leads` join can
// compare against uuid, the entry-scoped projection needs text.
var contactRefs = map[shared.EntryType]string{
	// The entry already stores its lead: this channel's conversations are
	// created FROM a campaign targeting one.
	shared.EntryTypeWhatsApp: "wce.lead_id",

	// An IGSID and a Telegram user id are opaque to everything outside their own
	// channel, so there is no lead to point at and the contact id rides the slot
	// instead. The usecase layer resolves these through the channel's registered
	// identity lookup.
	shared.EntryTypeInstagram: "igc.contact_id",
	shared.EntryTypeTelegram:  "tgc.contact_id",

	// The one channel where the contact IS a lead — same E.164 number, same row
	// in `leads` — so the CRM lead wins as soon as the contact resolves to one.
	//
	// The contact id is the fallback rather than NULL, and that is load-bearing
	// in the other direction: the identity hydration looks the contact up by
	// whatever is in this slot, and a group (never linked to a lead) or a
	// contact whose first message has not resolved yet still has to render with
	// a name, an avatar and its group flag.
	shared.EntryTypeUnofficialWhatsApp: `COALESCE(
		(SELECT uwct.lead_id FROM unofficial_whatsapp_contacts uwct
		  WHERE uwct.id = uwc.contact_id AND uwct.lead_id IS NOT NULL),
		uwc.contact_id)`,

	// Support conversations are with a person in the workspace, not a contact.
	shared.EntryTypeSupport: "NULL::uuid",
}

// contactRefUUID is the expression for a query that compares or unions on uuid,
// which is every workspace-wide branch: the UNION resolves to one type across
// its branches, and `LEFT JOIN leads l ON l.id = ae.lead_id` compares against a
// uuid column. A text cast anywhere in that chain drags the whole union to text
// and the join fails outright.
func contactRefUUID(entryType shared.EntryType) string {
	if ref, ok := contactRefs[entryType]; ok {
		return ref
	}
	return "NULL::uuid"
}

// contactRefText is the same expression for the entry-scoped projections, which
// scan into a Go string.
func contactRefText(entryType shared.EntryType) string {
	return "(" + contactRefUUID(entryType) + ")::text"
}
