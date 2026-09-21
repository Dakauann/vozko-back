package whatsapp_campaign_entry

import (
	"context"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/export"
	"vozko/infra/database/schema"
)

const exportPageSize = 1000

type exportRepository struct {
	db *gorm.DB
}

func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

type exportRow struct {
	EntryID      string              `gorm:"column:entry_id"`
	CampaignID   string              `gorm:"column:campaign_id"`
	CampaignName string              `gorm:"column:campaign_name"`
	Status       string              `gorm:"column:status"`
	ErrorCode    int                 `gorm:"column:error_code"`
	ErrorMessage string              `gorm:"column:error_message"`
	CreatedAt    time.Time           `gorm:"column:created_at"`
	UpdatedAt    time.Time           `gorm:"column:updated_at"`
	Variables    pq.StringArray      `gorm:"column:variables;type:text[]"`
	Metadata     schema.LeadMetadata `gorm:"column:metadata;type:jsonb"`
	LeadNumber   string              `gorm:"column:lead_number"`
	LeadName     string              `gorm:"column:lead_name"`
	LeadAge      *int                `gorm:"column:lead_age"`
}

func (r *exportRepository) ListForExport(
	ctx context.Context,
	scope export.Scope,
	emit func(export.ChannelEntry) error,
) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}

	var (
		lastCampaignID string
		lastStatus     string
		lastCreatedAt  time.Time
		lastEntryID    string
		first          = true
	)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		query := r.baseQuery(ctx, scope)
		if !first {
			query = query.Where(
				"(e.campaign_id, e.status, e.created_at, e.id) > (?::uuid, ?::text, ?::timestamptz, ?::uuid)",
				lastCampaignID, lastStatus, lastCreatedAt, lastEntryID,
			)
		}

		var rows []exportRow
		if err := query.
			Order("e.campaign_id, e.status, e.created_at, e.id").
			Limit(exportPageSize).
			Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}

		for i := range rows {
			if err := emit(toChannelEntry(&rows[i])); err != nil {
				return err
			}
		}

		last := rows[len(rows)-1]
		lastCampaignID, lastStatus, lastCreatedAt, lastEntryID = last.CampaignID, last.Status, last.CreatedAt, last.EntryID
		first = false

		if len(rows) < exportPageSize {
			return nil
		}
	}
}

func (r *exportRepository) baseQuery(ctx context.Context, scope export.Scope) *gorm.DB {
	query := r.db.WithContext(ctx).
		Table("whatsapp_campaign_entries AS e").
		Select(`e.id AS entry_id,
			e.campaign_id,
			c.name AS campaign_name,
			e.status,
			e.error_code,
			COALESCE(e.error_message, '') AS error_message,
			e.created_at,
			e.updated_at,
			e.variables,
			e.metadata,
			COALESCE(l.number, '') AS lead_number,
			COALESCE(l.name, '') AS lead_name,
			l.age AS lead_age`).
		Joins("JOIN whatsapp_campaigns c ON c.id = e.campaign_id AND c.deleted_at IS NULL").
		Joins("LEFT JOIN leads l ON l.id = e.lead_id AND l.deleted_at IS NULL AND l.workspace_id = c.workspace_id").
		Where("c.workspace_id = ?", scope.WorkspaceID).
		Where("e.deleted_at IS NULL")

	if containerID := strings.TrimSpace(scope.ContainerID); containerID != "" {
		query = query.Where("e.campaign_id = ?", containerID)
	}
	if containerType := strings.TrimSpace(scope.ContainerType); containerType != "" {
		query = query.Where("c.type = ?", containerType)
	}
	if len(scope.Statuses) > 0 {
		query = query.Where("e.status IN ?", scope.Statuses)
	}
	if scope.CreatedFrom != nil {
		query = query.Where("c.created_at >= ?", *scope.CreatedFrom)
	}
	if scope.CreatedTo != nil {
		query = query.Where("c.created_at <= ?", *scope.CreatedTo)
	}
	if len(scope.DepartmentIDs) > 0 {
		query = query.Where("c.department_id IN ?", scope.DepartmentIDs)
	}

	return query
}

func toChannelEntry(row *exportRow) export.ChannelEntry {
	return export.ChannelEntry{
		EntryID:       row.EntryID,
		Number:        row.LeadNumber,
		Name:          row.LeadName,
		Age:           row.LeadAge,
		ContainerName: row.CampaignName,
		Status:        row.Status,
		FailureCode:   row.ErrorCode,
		FailureReason: row.ErrorMessage,
		CreatedAt:     row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Format(time.RFC3339),
		Variables:     []string(row.Variables),
		Metadata:      map[string]interface{}(row.Metadata),
	}
}
