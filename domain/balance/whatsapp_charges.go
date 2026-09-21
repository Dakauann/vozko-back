package balance

import "time"

type WhatsAppChargeFilter struct {
	WorkspaceID   string
	DepartmentIDs []string
	CampaignType  string
	From          *time.Time
	To            *time.Time
}

type WhatsAppChargeStats struct {
	NetDispatches  int64
	Marketing      int64
	Utility        int64
	Authentication int64
}

type WhatsAppChargeAggregator interface {
	AggregateWhatsAppTemplateCharges(filter WhatsAppChargeFilter) (*WhatsAppChargeStats, error)
}
