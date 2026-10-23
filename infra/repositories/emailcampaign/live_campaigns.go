package emailcampaign

import (
	"context"

	"gorm.io/gorm"

	domain "vozko/domain/emailcampaign"
	"vozko/infra/database/schema"
)

type LiveCampaigns struct {
	db *gorm.DB
}

func NewLiveCampaigns(db *gorm.DB) *LiveCampaigns {
	return &LiveCampaigns{db: db}
}

func (r *LiveCampaigns) HasLiveCampaign(ctx context.Context, workspaceID string) (bool, error) {
	statuses := make([]string, 0, len(domain.LiveStatuses()))
	for _, s := range domain.LiveStatuses() {
		statuses = append(statuses, string(s))
	}
	var found []string
	err := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).
		Where("workspace_id = ? AND status IN ?", workspaceID, statuses).
		Limit(1).Pluck("id", &found).Error
	if err != nil {
		return false, err
	}
	return len(found) > 0, nil
}
