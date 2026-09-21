package conversation_repository

import "vozko/domain/shared"

var contactRefs = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp: "wce.lead_id",

	shared.EntryTypeInstagram: "igc.contact_id",
	shared.EntryTypeTelegram:  "tgc.contact_id",

	shared.EntryTypeUnofficialWhatsApp: `COALESCE(
		(SELECT uwct.lead_id FROM unofficial_whatsapp_contacts uwct
		  WHERE uwct.id = uwc.contact_id AND uwct.lead_id IS NOT NULL),
		uwc.contact_id)`,

	shared.EntryTypeSupport: "NULL::uuid",
}

func contactRefUUID(entryType shared.EntryType) string {
	if ref, ok := contactRefs[entryType]; ok {
		return ref
	}
	return "NULL::uuid"
}

func contactRefText(entryType shared.EntryType) string {
	return "(" + contactRefUUID(entryType) + ")::text"
}
