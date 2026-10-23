package repotest

import "vozko/infra/database/schema"

func LeadStoreModels() []any {
	models := []any{
		&schema.Lead{}, &schema.LeadEvent{}, &schema.LeadPhone{}, &schema.LeadAddress{}, &schema.LeadRelation{}, &schema.LeadCompany{},
		&schema.WhatsAppCampaignEntry{}, &schema.UnofficialWhatsAppContact{}, &schema.UnofficialWhatsAppConversation{},
		&schema.TelegramContact{}, &schema.TelegramConversation{}, &schema.InstagramContact{}, &schema.InstagramConversation{},
		&schema.FacebookContact{}, &schema.FacebookConversation{}, &schema.WebchatVisitor{}, &schema.WebchatConversation{},
		&schema.MercadoLivreContact{}, &schema.MercadoLivreConversation{}, &schema.TikTokContact{}, &schema.TikTokConversation{},
		&schema.CallList{}, &schema.CallListItem{},
	}
	models = append(models, schema.EmailInboxModels()...)
	return append(models, schema.CarrierMessagingModels()...)
}
