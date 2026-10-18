package lead

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

const (
	insertCompanySQL = "INSERT INTO lead_companies (id, workspace_id, name, accepts_leads, image_media_id, created_by, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?::uuid, ?::uuid, ?, ?)"

	updateCompanySQL = "UPDATE lead_companies SET name = ?, accepts_leads = ?, image_media_id = ?::uuid, updated_at = ?" +
		" WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"

	companySelectSQL = "SELECT c.id, c.workspace_id, c.name, c.accepts_leads, c.image_media_id, c.created_by, c.created_at, c.updated_at," +
		" m.url AS image_url, m.preview_url AS image_preview_url," +
		" (SELECT COUNT(*) FROM leads l WHERE l.workspace_id = c.workspace_id AND l.company_id = c.id AND l.deleted_at IS NULL) AS members_count" +
		" FROM lead_companies c" +
		" LEFT JOIN medias m ON m.id = c.image_media_id AND m.workspace_id = c.workspace_id AND m.deleted_at IS NULL" +
		" WHERE c.workspace_id = ? AND c.deleted_at IS NULL"

	companyCountSQL = "SELECT COUNT(*) FROM lead_companies c WHERE c.workspace_id = ? AND c.deleted_at IS NULL"

	companyPageSQL = " ORDER BY lower(c.name), c.id LIMIT ? OFFSET ?"

	lockCompanyForDeleteSQL = "SELECT id FROM lead_companies WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL FOR UPDATE"

	deleteCompanySQL = "UPDATE lead_companies SET deleted_at = ?, updated_at = ? WHERE workspace_id = ? AND id = ?"

	releaseMembersSQL = "UPDATE leads SET company_id = NULL, updated_at = ?, version = version + 1" +
		" WHERE workspace_id = ? AND company_id = ? AND deleted_at IS NULL RETURNING id::text AS lead_id, version"
)

var companySearchSQL = " AND " + database.SearchFold("c.name") + " LIKE " + database.SearchFold("?")

type companyRow struct {
	ID              string
	WorkspaceID     string
	Name            string
	AcceptsLeads    bool
	ImageMediaID    schema.OptionalText
	CreatedBy       schema.OptionalText
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ImageURL        schema.OptionalText
	ImagePreviewURL schema.OptionalText
	MembersCount    int
}

func (row companyRow) toDomain() leadcompany.Company {
	c := leadcompany.Company{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		Name:         row.Name,
		AcceptsLeads: row.AcceptsLeads,
		ImageMediaID: string(row.ImageMediaID),
		MembersCount: row.MembersCount,
		CreatedBy:    string(row.CreatedBy),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	if row.ImageMediaID != "" && row.ImageURL != "" {
		c.Image = &leadcompany.Image{MediaID: c.ImageMediaID, URL: string(row.ImageURL), PreviewURL: string(row.ImagePreviewURL)}
	}
	return c
}

func (r *repository) CreateCompany(ctx context.Context, c leadcompany.Company) error {
	return r.db.WithContext(ctx).Exec(insertCompanySQL,
		c.ID, c.WorkspaceID, c.Name, c.AcceptsLeads, schema.OptionalText(c.ImageMediaID), schema.OptionalText(c.CreatedBy), c.CreatedAt, c.UpdatedAt,
	).Error
}

func (r *repository) UpdateCompany(ctx context.Context, c leadcompany.Company) error {
	result := r.db.WithContext(ctx).Exec(updateCompanySQL,
		c.Name, c.AcceptsLeads, schema.OptionalText(c.ImageMediaID), c.UpdatedAt, c.WorkspaceID, c.ID,
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return leadcompany.ErrNotFound
	}
	return nil
}

func (r *repository) GetCompany(ctx context.Context, workspaceID, id string) (leadcompany.Company, error) {
	if !validUUIDs(workspaceID, id) {
		return leadcompany.Company{}, leadcompany.ErrNotFound
	}
	var rows []companyRow
	if err := r.db.WithContext(ctx).Raw(companySelectSQL+" AND c.id = ?", workspaceID, id).Scan(&rows).Error; err != nil {
		return leadcompany.Company{}, err
	}
	if len(rows) == 0 {
		return leadcompany.Company{}, leadcompany.ErrNotFound
	}
	return rows[0].toDomain(), nil
}

func (r *repository) ListCompanies(ctx context.Context, workspaceID string, q leadcompany.ListQuery) (*shared.PaginatedResult[leadcompany.Company], error) {
	page := shared.NormalizePaginationWithin(q.Pagination(), leadcompany.MaxPageSize)
	if !validUUIDs(workspaceID) {
		return shared.NewPaginatedResultWithin([]leadcompany.Company{}, page, 0, leadcompany.MaxPageSize), nil
	}
	filter, args := "", []interface{}{workspaceID}
	if search := strings.TrimSpace(q.Search); search != "" {
		filter, args = companySearchSQL, append(args, sqlexpr.LikeContains(search))
	}
	db := r.db.WithContext(ctx)
	var total int64
	if err := db.Raw(companyCountSQL+filter, args...).Scan(&total).Error; err != nil {
		return nil, err
	}
	var rows []companyRow
	if err := db.Raw(companySelectSQL+filter+companyPageSQL, append(args, page.PageSize, page.OffsetWithin(leadcompany.MaxPageSize))...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]leadcompany.Company, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.toDomain())
	}
	return shared.NewPaginatedResultWithin(items, page, total, leadcompany.MaxPageSize), nil
}

type releasedRow struct {
	LeadID  string
	Version int64
}

func (r *repository) DeleteCompany(ctx context.Context, workspaceID, id string, release recordevent.Event, at time.Time) ([]leadcompany.ReleasedLead, error) {
	if !validUUIDs(workspaceID, id) {
		return nil, leadcompany.ErrNotFound
	}
	var released []leadcompany.ReleasedLead
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []string
		if err := tx.Raw(lockCompanyForDeleteSQL, workspaceID, id).Scan(&locked).Error; err != nil {
			return err
		}
		if len(locked) == 0 {
			return leadcompany.ErrNotFound
		}
		if err := tx.Exec(deleteCompanySQL, at, at, workspaceID, id).Error; err != nil {
			return err
		}
		var rows []releasedRow
		if err := tx.Raw(releaseMembersSQL, at, workspaceID, id).Scan(&rows).Error; err != nil {
			return err
		}
		var events []schema.LeadEvent
		for _, row := range rows {
			eventsOfLead, err := eventRows(workspaceID, row.LeadID, []recordevent.Event{release}, at)
			if err != nil {
				return err
			}
			events = append(events, eventsOfLead...)
			released = append(released, leadcompany.ReleasedLead{LeadID: row.LeadID, Version: row.Version})
		}
		return insertEventRows(tx, events)
	})
	if err != nil {
		return nil, err
	}
	if len(released) > 0 {
		r.agg.bump(workspaceID)
	}
	return released, nil
}

func validUUIDs(ids ...string) bool {
	for _, id := range ids {
		if len(database.UUIDArray([]string{id})) == 0 {
			return false
		}
	}
	return true
}
