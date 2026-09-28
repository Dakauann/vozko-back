package conversation_repository

func UnofficialCampaignJoin(conv, camp string) string {
	return "LEFT JOIN unofficial_whatsapp_campaigns " + camp +
		" ON " + camp + ".id = NULLIF(" + conv + ".campaign_id, '')::uuid" +
		" AND " + camp + ".deleted_at IS NULL"
}
