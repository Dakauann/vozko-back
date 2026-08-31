package unofficial_whatsapp_campaign_repository

import (
	"gorm.io/gorm"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/database/schema"
)

// summaryRepository rolls entry statuses up to the workspace level.
//
// Separate from EntryRepository, mirroring the official channel, so adding the
// summary does not force every repository double in the test suite to grow a
// method it will never use.
type summaryRepository struct{ db *gorm.DB }

func NewSummaryRepository(db *gorm.DB) uwc.SummaryAggregator {
	return &summaryRepository{db: db}
}

// CountByStatusForWorkspace aggregates in ONE query across every campaign the
// filter selects.
//
// The date range bounds the CAMPAIGN's creation, not the entry's, so the tiles
// and the export answer the same question — the same contract the official
// summary carries.
func (r *summaryRepository) CountByStatusForWorkspace(filter uwc.WorkspaceSummaryFilter) (*campaign.Counts, error) {
	type row struct {
		Status string
		Total  int64
	}

	query := r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Select("unofficial_whatsapp_campaign_entries.status AS status, COUNT(*) AS total").
		Joins("JOIN unofficial_whatsapp_campaigns c ON c.id = unofficial_whatsapp_campaign_entries.campaign_id AND c.deleted_at IS NULL").
		Where("c.workspace_id = ?", filter.WorkspaceID).
		Where("unofficial_whatsapp_campaign_entries.deleted_at IS NULL")

	if len(filter.DepartmentIDs) > 0 {
		query = query.Where("c.department_id IN ?", filter.DepartmentIDs)
	}
	if len(filter.InstanceIDs) > 0 {
		query = query.Where("c.instance_id IN ?", filter.InstanceIDs)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("c.created_at >= ?", filter.CreatedFrom.UTC())
	}
	if filter.CreatedTo != nil {
		query = query.Where("c.created_at <= ?", filter.CreatedTo.UTC())
	}

	var rows []row
	if err := query.Group("unofficial_whatsapp_campaign_entries.status").Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := &campaign.Counts{}
	for _, rec := range rows {
		addStatusCount(counts, campaign.SendStatus(rec.Status), rec.Total)
	}
	return counts, nil
}
