package conversation_repository

import (
	"fmt"
	"sort"

	"vozko/domain/shared"
)

var contactRefs = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp: "wce.lead_id",

	shared.EntryTypeInstagram: "igc.contact_id",
	shared.EntryTypeTelegram:  "tgc.contact_id",
	shared.EntryTypeFacebook:  "fbc.contact_id",

	shared.EntryTypeMercadoLivre: "mlc.contact_id",
	shared.EntryTypeTikTok:       "ttc.contact_id",

	shared.EntryTypeWebchat: `COALESCE(
		(SELECT wcv.lead_id FROM webchat_visitors wcv
		  WHERE wcv.id = wcc.visitor_id AND wcv.lead_id IS NOT NULL),
		wcc.visitor_id)`,

	shared.EntryTypeEmail: `COALESCE(
		(SELECT ect.lead_id FROM email_contacts ect
		  WHERE ect.id = emc.contact_id AND ect.lead_id IS NOT NULL),
		emc.contact_id)`,

	shared.EntryTypeRCS: `COALESCE(
		(SELECT cct.lead_id FROM carrier_contacts cct
		  WHERE cct.id = rcc.contact_id AND cct.lead_id IS NOT NULL),
		rcc.contact_id)`,

	shared.EntryTypeSMS: `COALESCE(
		(SELECT cct.lead_id FROM carrier_contacts cct
		  WHERE cct.id = smc.contact_id AND cct.lead_id IS NOT NULL),
		smc.contact_id)`,

	shared.EntryTypeUnofficialWhatsApp: `COALESCE(
		(SELECT uwct.lead_id FROM unofficial_whatsapp_contacts uwct
		  WHERE uwct.id = uwc.contact_id AND uwct.lead_id IS NOT NULL),
		uwc.contact_id)`,
}

func init() {
	if missing := entryTypesWithoutContactRef(); len(missing) > 0 {
		panic(fmt.Sprintf("conversation repository: no contact slot for %v; every entry type in the registries needs one in contactRefs", missing))
	}
}

func entryTypesWithoutContactRef() []shared.EntryType {
	seen := map[shared.EntryType]struct{}{}
	check := func(entryType shared.EntryType) {
		if _, ok := contactRefs[entryType]; !ok {
			seen[entryType] = struct{}{}
		}
	}
	for _, entryType := range shared.KnownEntryTypes() {
		check(entryType)
	}
	for _, src := range entrySources {
		check(src.EntryType)
	}
	for _, q := range channelQueries {
		check(q.EntryType)
	}
	missing := make([]shared.EntryType, 0, len(seen))
	for entryType := range seen {
		missing = append(missing, entryType)
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing
}

func contactRefUUID(entryType shared.EntryType) string {
	ref, ok := contactRefs[entryType]
	if !ok {
		panic(fmt.Sprintf("conversation repository: no contact slot for entry type %q", entryType))
	}
	return ref
}

func contactRefText(entryType shared.EntryType) string {
	return "(" + contactRefUUID(entryType) + ")::text"
}
