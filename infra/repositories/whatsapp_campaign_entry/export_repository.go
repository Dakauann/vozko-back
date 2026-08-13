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

// exportPageSize is how many rows one query returns.
//
// The walk is paged rather than streamed from an open cursor on purpose: the
// pool is 10 connections wide, and a workspace-wide export that held one of
// them for the length of the download would be a tenant starving every other
// request on the instance. Each page is a short query that returns its
// connection immediately.
const exportPageSize = 1000

type exportRepository struct {
	db *gorm.DB
}

// NewExportRepository builds the WhatsApp export source.
//
// WhatsApp used to be special-cased inside the export usecase: it was the one
// channel that could not be a ChannelEntryLister, because the port took a
// container id and nothing else, and a campaign export needs statuses and a
// period. With the port carrying a Scope, WhatsApp is an ordinary channel, and
// exporting every campaign at once is the same code path as exporting one.
func NewExportRepository(db *gorm.DB) export.ChannelEntryLister {
	return &exportRepository{db: db}
}

type exportRow struct {
	EntryID      string              `gorm:"column:entry_id"`
	CampaignID   string              `gorm:"column:campaign_id"`
	CampaignName string              `gorm:"column:campaign_name"`
	Status       string              `gorm:"column:status"`
	CreatedAt    time.Time           `gorm:"column:created_at"`
	UpdatedAt    time.Time           `gorm:"column:updated_at"`
	Variables    pq.StringArray      `gorm:"column:variables;type:text[]"`
	Metadata     schema.LeadMetadata `gorm:"column:metadata;type:jsonb"`
	LeadNumber   string              `gorm:"column:lead_number"`
	LeadName     string              `gorm:"column:lead_name"`
	LeadAge      *int                `gorm:"column:lead_age"`
}

// ListForExport walks one campaign's entries, or every campaign in the scope.
//
// Rows come out ordered by (campaign_id, status, created_at, id), which is the
// leading edge of idx_wce_campaign_status_created. That ordering is not a
// presentation choice: it is what lets each page be an index scan with a keyset
// predicate instead of an OFFSET that re-reads and re-sorts everything before
// it. A workspace-wide export therefore costs one index walk in total, not one
// per page, and it groups the file by campaign, which is what makes a
// multi-campaign export readable.
func (r *exportRepository) ListForExport(
	ctx context.Context,
	scope export.Scope,
	emit func(export.ChannelEntry) error,
) error {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if workspaceID == "" {
		return nil
	}

	// Keyset cursor. Zero values sort before every real row, so the first page
	// needs no special case.
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
			e.created_at,
			e.updated_at,
			e.variables,
			e.metadata,
			COALESCE(l.number, '') AS lead_number,
			COALESCE(l.name, '') AS lead_name,
			l.age AS lead_age`).
		Joins("JOIN whatsapp_campaigns c ON c.id = e.campaign_id AND c.deleted_at IS NULL").
		// A lead can be deleted while its entry survives. LEFT JOIN keeps that
		// row in the file with a blank identity rather than dropping a send the
		// summary tiles still count — the file has to reconcile with the tiles.
		// The workspace predicate on the join is a second tenancy barrier: even
		// a mis-keyed lead_id cannot pull in another tenant's contact.
		Joins("LEFT JOIN leads l ON l.id = e.lead_id AND l.deleted_at IS NULL AND l.workspace_id = c.workspace_id").
		// Tenancy is enforced here, not by the caller: an operator must not be
		// able to export another workspace's campaign by guessing its id.
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
	// Same bounds, on the same column, as the disparos summary: the campaign's
	// creation date. Filtering entries by their own date instead would answer a
	// different question and quietly disagree with the tiles.
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
		CreatedAt:     row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Format(time.RFC3339),
		Variables:     []string(row.Variables),
		Metadata:      map[string]interface{}(row.Metadata),
	}
}
