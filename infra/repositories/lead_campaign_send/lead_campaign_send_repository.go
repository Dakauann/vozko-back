package lead_campaign_send

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domain "vozko/domain/lead_campaign_send"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func LastSentSQL(leadIn string) string {
	return "SELECT s.lead_id, MAX(s.sent_at) AS last_sent_at FROM lead_campaign_sends s WHERE s.lead_id " + leadIn + " AND s.business_phone_id = ?::uuid GROUP BY s.lead_id"
}

func NewRepository(db *gorm.DB) domain.Repository {
	return &repository{db: db}
}

func (r *repository) Record(leadID, businessPhoneID, campaignID string) error {
	record := &schema.LeadCampaignSend{
		ID:              uuid.New().String(),
		LeadID:          leadID,
		BusinessPhoneID: businessPhoneID,
		CampaignID:      campaignID,
		SentAt:          time.Now().UTC(),
	}
	return r.db.Create(record).Error
}

func (r *repository) GetLastSendTime(leadID, businessPhoneID string) (*time.Time, error) {
	var record schema.LeadCampaignSend
	err := r.db.
		Where("lead_id = ? AND business_phone_id = ?", leadID, businessPhoneID).
		Order("sent_at DESC").
		First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &record.SentAt, nil
}

func (r *repository) GetLastSendTimesBatch(ctx context.Context, leadIDs []string, businessPhoneID string) (map[string]time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	valid := database.UUIDsOf(leadIDs)
	if len(valid) == 0 {
		return make(map[string]time.Time), nil
	}

	var results []struct {
		LeadID     string
		LastSentAt time.Time
	}

	err := r.db.WithContext(ctx).Raw(LastSentSQL("= ANY(?::uuid[])"), valid, businessPhoneID).Find(&results).Error
	if err != nil {
		return nil, err
	}

	m := make(map[string]time.Time, len(results))
	for _, r := range results {
		m[r.LeadID] = r.LastSentAt
	}
	return m, nil
}
