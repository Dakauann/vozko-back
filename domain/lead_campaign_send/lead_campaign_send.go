package lead_campaign_send

import (
	"time"
)

type LeadCampaignSend struct {
	ID              string    `json:"id"`
	LeadID          string    `json:"leadId"`
	BusinessPhoneID string    `json:"businessPhoneId"`
	CampaignID      string    `json:"campaignId"`
	SentAt          time.Time `json:"sentAt"`
}

type Repository interface {
	Record(leadID, businessPhoneID, campaignID string) error

	GetLastSendTime(leadID, businessPhoneID string) (*time.Time, error)

	GetLastSendTimesBatch(leadIDs []string, businessPhoneID string) (map[string]time.Time, error)
}
