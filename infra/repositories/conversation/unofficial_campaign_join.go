package conversation_repository

// UnofficialCampaignJoin joins an unofficial WhatsApp conversation (alias conv)
// to the campaign it was opened for (alias camp), or to nothing for a
// campaign-less conversation or a deleted campaign. It is the one definition of
// "a conversation's campaign": the inbox, the automation profile and the
// department and campaign lookups all read it, and it is known from the moment
// the conversation exists, before any send is recorded.
func UnofficialCampaignJoin(conv, camp string) string {
	return "LEFT JOIN unofficial_whatsapp_campaigns " + camp +
		" ON " + camp + ".id = NULLIF(" + conv + ".campaign_id, '')::uuid" +
		" AND " + camp + ".deleted_at IS NULL"
}
