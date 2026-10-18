package whatsapp_campaign

import "vozko/domain/campaign"

type StartScheduleJob interface {
	StartScheduledWhatsappCampaigns() error
}

type ScheduleClaimer = campaign.ScheduleClaimer
