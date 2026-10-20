package whatsapp_campaign_entry

import (
	"strings"

	"github.com/lib/pq"

	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/database/schema"
)

func (r *repository) EachStored(campaignID string, visit func(wce.StoredEntry) error) error {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return wce.ErrEntryCampaignRequired
	}

	rows, err := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Select("lead_id, status, variables, metadata").
		Where("campaign_id = ?", campaignID).
		Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			leadID    string
			status    string
			variables pq.StringArray
			metadata  schema.LeadMetadata
		)
		if err := rows.Scan(&leadID, &status, &variables, &metadata); err != nil {
			return err
		}
		stored := wce.StoredEntry{
			LeadID:    leadID,
			Status:    wce.SendStatus(status),
			Variables: []string(variables),
			Metadata:  map[string]interface{}(metadata),
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	return rows.Err()
}
