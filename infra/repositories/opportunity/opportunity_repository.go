// Package opportunity_repository is the GORM implementation of the domain
// opportunity.Repository (deal lifecycle) and opportunity.LinkRepository
// (deal <-> conversation links). Schema rows live in infra/database/schema and
// are hand-mapped here; the constructor returns the domain interface. Filtered
// board/list reads are served by the shared crmfilter compiler
// (OpportunityDescriptor), not by this port.
package opportunity_repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
	crmfiltersql "vozko/infra/repositories/crmfilter"
)

// ErrNotFound is returned when an opportunity row does not exist (or is not
// visible in the given workspace). The domain opportunity package defines no
// not-found sentinel, so it lives here; the HTTP handler maps it to 404.
var ErrNotFound = errors.New("opportunity: not found")

type repository struct {
	db *gorm.DB
}

// NewRepository returns the domain opportunity.Repository backed by GORM.
func NewRepository(db *gorm.DB) opportunity.Repository {
	return &repository{db: db}
}

func (r *repository) Create(o *opportunity.Opportunity) error {
	row, err := mapToSchema(o)
	if err != nil {
		return err
	}
	if err := r.db.Create(row).Error; err != nil {
		return err
	}
	o.ID = row.ID
	o.CreatedAt = row.CreatedAt
	o.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(o *opportunity.Opportunity) error {
	customJSON, err := marshalCustomFields(o.CustomFields)
	if err != nil {
		return err
	}
	update := map[string]interface{}{
		"lead_id":        nullableUUID(o.LeadID),
		"pipeline_id":    o.PipelineID,
		"stage_id":       o.StageID,
		"owner_id":       nullableUUID(o.OwnerID),
		"carteira_id":    nullableUUID(o.CarteiraID),
		"title":          o.Title,
		"value_cents":    o.ValueCents,
		"currency":       o.Currency,
		"status":         string(o.Status),
		"lost_reason_id": nullableUUID(o.LostReasonID),
		"source":         o.Source,
		"close_date":     o.CloseDate,
		"custom_fields":  customJSON,
	}
	res := r.db.Model(&schema.Opportunity{}).
		Where("id = ? AND workspace_id = ?", o.ID, o.WorkspaceID).
		Updates(update)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) Delete(workspaceID, id string) error {
	res := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.Opportunity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) GetByID(workspaceID, id string) (*opportunity.Opportunity, error) {
	var row schema.Opportunity
	if err := r.db.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&row)
}

func (r *repository) ListByPipeline(workspaceID, pipelineID string) ([]*opportunity.Opportunity, error) {
	var rows []schema.Opportunity
	if err := r.db.
		Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*opportunity.Opportunity, 0, len(rows))
	for i := range rows {
		o, err := mapToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// ListByPipelineScoped is ListByPipeline with the same owner-department scope the
// board/list apply, so user-facing reads over a whole pipeline (the flat GET and the
// CSV export) cannot leak deals across departments. Pass restrict=false for
// admins/owners (workspace-wide).
func (r *repository) ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*opportunity.Opportunity, error) {
	q := r.db.Table("opportunities "+oppAlias).
		Where(oppAlias+".deleted_at IS NULL").
		Where(oppAlias+".workspace_id = ?", workspaceID).
		Where(oppAlias+".pipeline_id = ?", pipelineID)
	q = applyDepartmentScopeRaw(q, departmentIDs, restrict, assigneeOverrideUserID)
	var rows []schema.Opportunity
	if err := q.Order(oppAlias + ".created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*opportunity.Opportunity, 0, len(rows))
	for i := range rows {
		o, err := mapToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// oppAlias is the table alias the crmfilter OpportunityDescriptor emits its
// column expressions against (e.g. "o.stage_id = ANY(?)"), so every filtered
// query below aliases the opportunities table as "o".
const oppAlias = "o"

// filteredQuery builds the shared, workspace-scoped, soft-delete-aware base query
// with the compiled crmfilter WHERE appended. The table is aliased "o" so the
// OpportunityDescriptor's "o.<col>" expressions resolve. Soft deletes are excluded
// manually because .Table() (unlike .Model()) does not auto-apply the deleted_at
// scope.
func (r *repository) filteredQuery(input opportunity.SearchByFilterInput) (*gorm.DB, error) {
	wsID := strings.TrimSpace(input.WorkspaceID)
	if wsID == "" {
		return nil, opportunity.ErrWorkspaceRequired
	}
	whereSQL, whereArgs, err := crmfiltersql.CompileOpportunity(input.Filter, crmfiltersql.NewOpportunityDescriptor(), 1)
	if err != nil {
		return nil, err
	}
	q := r.db.Table("opportunities "+oppAlias).
		Where(oppAlias+".deleted_at IS NULL").
		Where(oppAlias+".workspace_id = ?", wsID)
	if strings.TrimSpace(whereSQL) != "" {
		q = q.Where(whereSQL, whereArgs...)
	}
	q = applyDepartmentScope(q, input)
	return q, nil
}

// applyDepartmentScope restricts the deal board/list to a department-scoped user:
// a deal is visible when the requesting user OWNS it, OR the deal's owner belongs to
// one of the user's departments. Mirrors the conversation departmentScopeClause
// (owner ~ assignee). Fail-closed: a restricted user with neither an owner-override
// nor departments sees nothing. Unassigned deals (owner NULL) are invisible to
// restricted users by design — admins/owners (RestrictDepartments=false) see all.
func applyDepartmentScope(q *gorm.DB, input opportunity.SearchByFilterInput) *gorm.DB {
	return applyDepartmentScopeRaw(q, input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
}

// applyDepartmentScopeRaw is the alias-"o" scope clause, shared by the filtered
// board/list queries and the whole-pipeline reads (flat list + CSV export).
func applyDepartmentScopeRaw(q *gorm.DB, departmentIDs []string, restrict bool, assigneeOverride string) *gorm.DB {
	if !restrict {
		return q
	}
	var conds []string
	var args []interface{}
	if assigneeOverride != "" {
		conds = append(conds, oppAlias+".owner_id = ?")
		args = append(args, assigneeOverride)
	}
	if len(departmentIDs) > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM workspace_department_members wdm "+
			"JOIN workspace_members wm ON wm.id = wdm.member_id "+
			"WHERE wm.user_id = "+oppAlias+".owner_id AND wdm.department_id = ANY(?::uuid[]))")
		args = append(args, pq.Array(departmentIDs))
	}
	if len(conds) == 0 {
		return q.Where("1 = 0") // fail-closed
	}
	return q.Where("("+strings.Join(conds, " OR ")+")", args...)
}

// orderClause maps the SearchByFilterInput sort onto an ORDER BY over the "o"
// alias, with a stable "o.id" tiebreaker so pagination is deterministic.
func orderClause(sortField, sortOrder string) string {
	dir := "DESC"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "asc") {
		dir = "ASC"
	}
	switch strings.TrimSpace(sortField) {
	case "value":
		return fmt.Sprintf("%s.value_cents %s, %s.id ASC", oppAlias, dir, oppAlias)
	case "close_date":
		// close_date is nullable; keep dated deals grouped ahead of undated ones.
		return fmt.Sprintf("%s.close_date %s NULLS LAST, %s.id ASC", oppAlias, dir, oppAlias)
	default: // "created", "created_at", ""
		return fmt.Sprintf("%s.created_at %s, %s.id ASC", oppAlias, dir, oppAlias)
	}
}

// SearchByFilter returns a page of opportunities matching the compiled filter
// (workspace-scoped, newest-first by default) plus the total match count.
func (r *repository) SearchByFilter(input opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error) {
	q, err := r.filteredQuery(input)
	if err != nil {
		return nil, 0, err
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("opportunity: counting filtered results: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	page := input.Page
	if page <= 0 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	var rows []schema.Opportunity
	if err := q.
		Order(orderClause(input.SortField, input.SortOrder)).
		Limit(pageSize).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("opportunity: searching filtered results: %w", err)
	}

	out := make([]*opportunity.Opportunity, 0, len(rows))
	for i := range rows {
		o, err := mapToDomain(&rows[i])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, nil
}

// SumValueByFilter returns COALESCE(SUM(value_cents), 0) across all matching
// rows. MONEY GUARDRAIL: value_cents is the customer's own BRL sales figure and
// never touches the USD ledger.
func (r *repository) SumValueByFilter(input opportunity.SearchByFilterInput) (int64, error) {
	q, err := r.filteredQuery(input)
	if err != nil {
		return 0, err
	}
	var sum int64
	if err := q.Select("COALESCE(SUM(" + oppAlias + ".value_cents), 0)").Scan(&sum).Error; err != nil {
		return 0, fmt.Errorf("opportunity: summing filtered value: %w", err)
	}
	return sum, nil
}

func mapToSchema(o *opportunity.Opportunity) (*schema.Opportunity, error) {
	customJSON, err := marshalCustomFields(o.CustomFields)
	if err != nil {
		return nil, err
	}
	return &schema.Opportunity{
		ID:           o.ID,
		WorkspaceID:  o.WorkspaceID,
		LeadID:       o.LeadID,
		PipelineID:   o.PipelineID,
		StageID:      o.StageID,
		OwnerID:      o.OwnerID,
		CarteiraID:   o.CarteiraID,
		Title:        o.Title,
		ValueCents:   o.ValueCents,
		Currency:     o.Currency,
		Status:       string(o.Status),
		LostReasonID: o.LostReasonID,
		Source:       o.Source,
		CloseDate:    o.CloseDate,
		CustomFields: customJSON,
	}, nil
}

func mapToDomain(row *schema.Opportunity) (*opportunity.Opportunity, error) {
	custom, err := unmarshalCustomFields(row.CustomFields)
	if err != nil {
		return nil, err
	}
	return &opportunity.Opportunity{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		LeadID:       row.LeadID,
		PipelineID:   row.PipelineID,
		StageID:      row.StageID,
		OwnerID:      row.OwnerID,
		CarteiraID:   row.CarteiraID,
		Title:        row.Title,
		ValueCents:   row.ValueCents,
		Currency:     row.Currency,
		Status:       opportunity.Status(row.Status),
		LostReasonID: row.LostReasonID,
		Source:       row.Source,
		CloseDate:    row.CloseDate,
		CustomFields: custom,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// nullableUUID returns nil for an empty id so an Update writes SQL NULL instead
// of an empty string into a nullable uuid column (which Postgres would reject).
func nullableUUID(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func marshalCustomFields(m map[string]any) (datatypes.JSON, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func unmarshalCustomFields(raw datatypes.JSON) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
