package carriercampaign_repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	ccuc "vozko/usecases/carriercampaign"
)

type RecipientRowReader struct {
	db *gorm.DB
}

var _ ccuc.RecipientReader = (*RecipientRowReader)(nil)

func NewRecipientRowReader(db *gorm.DB) *RecipientRowReader {
	return &RecipientRowReader{db: db}
}

type recipientRowRecord struct {
	schema.CarrierCampaignRecipient
	LeadName      string `gorm:"column:lead_name"`
	MessageStatus string `gorm:"column:message_status"`
}

func recipientRowFilter(q ccuc.RecipientQuery) (string, []any, error) {
	if !wellFormedID(q.CampaignID) {
		return "", nil, carriercampaign.ErrCampaignNotFound
	}
	conds, args := []string{"r.workspace_id = ?", "r.campaign_id = ?"}, []any{q.WorkspaceID, q.CampaignID}
	if q.RunID != "" {
		if !wellFormedID(q.RunID) {
			return "", nil, carriercampaign.ErrRunNotFound
		}
		conds, args = append(conds, "r.run_id = ?"), append(args, q.RunID)
	}
	if q.Status != "" {
		if err := valid("recipient status", q.Status); err != nil {
			return "", nil, err
		}
		conds, args = append(conds, "r.status = ?"), append(args, q.Status)
	}
	return strings.Join(conds, " AND "), args, nil
}

func (r *RecipientRowReader) ListRecipientRows(ctx context.Context, q ccuc.RecipientQuery) (*shared.PaginatedResult[ccuc.RecipientRow], error) {
	where, args, err := recipientRowFilter(q)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM carrier_campaign_recipients r WHERE `+where, args...).Scan(&total).Error; err != nil {
		return nil, err
	}
	page := shared.NormalizePagination(q.Pagination)
	var records []recipientRowRecord
	if err := r.db.WithContext(ctx).Raw(`SELECT r.*, COALESCE(l.name, '') AS lead_name, COALESCE(m.status, '') AS message_status
FROM carrier_campaign_recipients r
LEFT JOIN leads l ON l.id = r.lead_id
LEFT JOIN carrier_messages m ON m.id = r.message_id
WHERE `+where+`
ORDER BY r.created_at ASC, r.id ASC LIMIT ? OFFSET ?`, append(args, page.PageSize, page.Offset())...).Scan(&records).Error; err != nil {
		return nil, err
	}
	items := make([]ccuc.RecipientRow, 0, len(records))
	for i := range records {
		recipient, err := toRecipient(&records[i].CarrierCampaignRecipient)
		if err != nil {
			return nil, err
		}
		status := carriermessaging.Status(records[i].MessageStatus)
		if err := validOrEmpty("message status", status); err != nil {
			return nil, err
		}
		items = append(items, ccuc.RecipientRow{Recipient: *recipient, LeadName: records[i].LeadName, MessageStatus: status})
	}
	return shared.NewPaginatedResult(items, page, total), nil
}
