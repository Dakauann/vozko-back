package database

import (
	"vozko/domain/calls/cdr"
	"vozko/domain/conversation"
)

func NotSeedPlaceholderSQL(alias string) string {
	return "NOT (" + alias + ".message_type = '" + string(conversation.MessageTypeSystem) +
		"' AND COALESCE(" + alias + ".metadata->>'" + conversation.SeedMetadataKey + "', '') = '" +
		conversation.SeedSourceLeadImport + "')"
}

func LiveMessageSQL(alias string) string {
	return "COALESCE(" + alias + ".metadata->>'" + conversation.BackfillMetadataKey + "', '') <> 'true'"
}

func RealMessageSQL(alias string) string {
	return NotSeedPlaceholderSQL(alias) + " AND " + LiveMessageSQL(alias)
}

func CallCounterpartSQL(alias string) string {
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	return "(CASE WHEN " + column("direction") + " = '" + string(cdr.DirectionInbound) + "' THEN " + column("phone_from") +
		" ELSE " + column("phone_to") + " END)"
}
