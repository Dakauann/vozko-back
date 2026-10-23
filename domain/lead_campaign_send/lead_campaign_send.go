package lead_campaign_send

import (
	"context"
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

	GetLastSendTimesBatch(ctx context.Context, leadIDs []string, businessPhoneID string) (map[string]time.Time, error)
}
