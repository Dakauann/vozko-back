package balance

import "time"

// WhatsAppChargeFilter selects ledger rows that bill WhatsApp template sends
// (service_type = whatsapp_campaign). Date bounds filter transaction created_at
// (when money moved), not campaign creation. Department/type join the campaign
// referenced by the debit (or refund:campaignId).
type WhatsAppChargeFilter struct {
	WorkspaceID   string
	DepartmentIDs []string
	CampaignType  string
	From          *time.Time
	To            *time.Time
}

// WhatsAppChargeStats is net billed template sends from the balance ledger:
// debits minus refunds. Survives campaign reset (entry status is unrelated).
type WhatsAppChargeStats struct {
	// NetDispatches is max(0, debits − refunds) across all categories.
	NetDispatches  int64
	Marketing      int64
	Utility        int64
	Authentication int64
}

// WhatsAppChargeAggregator is a narrow port so summary can read true charged
// volume without growing every balance.Repository fake. Implemented by the
// balance repository (and the cached wrapper).
type WhatsAppChargeAggregator interface {
	AggregateWhatsAppTemplateCharges(filter WhatsAppChargeFilter) (*WhatsAppChargeStats, error)
}
