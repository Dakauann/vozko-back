package workflow_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"
)

const deleteDeliveriesBeforeSQL = `DELETE FROM workflow_webhook_deliveries WHERE id IN (SELECT id FROM workflow_webhook_deliveries WHERE received_at < ? ORDER BY received_at LIMIT ?)`

type webhookDeliveryRepository struct {
	db *gorm.DB
}

func NewWebhookDeliveryRepository(db *gorm.DB) workflow.WebhookDeliveryRepository {
	return &webhookDeliveryRepository{db: db}
}

func (r *webhookDeliveryRepository) Record(d *workflow.WebhookDelivery) error {
	var columns jsonColumns
	row := &schema.WorkflowWebhookDeliverySchema{
		ID:              d.ID,
		WebhookID:       d.WebhookID,
		WorkflowID:      d.WorkflowID,
		WorkspaceID:     d.WorkspaceID,
		ReceivedAt:      d.ReceivedAt,
		Outcome:         string(d.Outcome),
		Reason:          d.Reason,
		HTTPStatus:      d.HTTPStatus,
		EntryResolution: d.EntryResolution,
		EntryID:         d.EntryID,
		EntryType:       d.EntryType,
		RunID:           nilIfEmpty(d.RunID),
		LatencyMs:       d.LatencyMs,
		PayloadKeys:     columns.of(d.PayloadKeys, len(d.PayloadKeys) > 0),
	}
	if columns.err != nil {
		return columns.err
	}
	return r.db.Create(row).Error
}

func (r *webhookDeliveryRepository) ListByWorkflow(workspaceID, workflowID string, visibility conversation.VisibilityScope, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.WebhookDelivery], error) {
	pagination = shared.NormalizePagination(pagination)
	query := r.db.Model(&schema.WorkflowWebhookDeliverySchema{}).Where("workspace_id = ? AND workflow_id = ?", workspaceID, workflowID)
	if visible, args := visibleDeliveryEntries(workspaceID, visibility); visible != "" {
		query = query.Where(visible, args...)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []schema.WorkflowWebhookDeliverySchema
	if err := query.Order("received_at DESC").Limit(pagination.PageSize).Offset(pagination.Offset()).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*workflow.WebhookDelivery, 0, len(rows))
	for i := range rows {
		d, err := mapDeliveryToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *webhookDeliveryRepository) DeleteReceivedBefore(cutoff time.Time, limit int) (int64, error) {
	result := r.db.Exec(deleteDeliveriesBeforeSQL, cutoff, limit)
	return result.RowsAffected, result.Error
}

func mapDeliveryToDomain(row *schema.WorkflowWebhookDeliverySchema) (*workflow.WebhookDelivery, error) {
	d := &workflow.WebhookDelivery{
		ID:              row.ID,
		WebhookID:       row.WebhookID,
		WorkflowID:      row.WorkflowID,
		WorkspaceID:     row.WorkspaceID,
		ReceivedAt:      row.ReceivedAt,
		Outcome:         workflow.WebhookOutcome(row.Outcome),
		Reason:          row.Reason,
		HTTPStatus:      row.HTTPStatus,
		EntryResolution: row.EntryResolution,
		EntryID:         row.EntryID,
		EntryType:       row.EntryType,
		LatencyMs:       row.LatencyMs,
	}
	if row.RunID != nil {
		d.RunID = *row.RunID
	}
	if err := decodeColumn(row.PayloadKeys, &d.PayloadKeys); err != nil {
		return nil, err
	}
	return d, nil
}
