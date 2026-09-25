package attendance_repository

import "vozko/domain/conversation"

func realMessageSQL(alias string) string {
	return "NOT (" + alias + ".message_type = '" + string(conversation.MessageTypeSystem) +
		"' AND COALESCE(" + alias + ".metadata->>'" + conversation.SeedMetadataKey + "', '') = '" +
		conversation.SeedSourceLeadImport + "')"
}
