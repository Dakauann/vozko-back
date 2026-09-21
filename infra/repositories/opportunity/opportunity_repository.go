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

var ErrNotFound = errors.New("opportunity: not found")

type repository struct {
	db *gorm.DB
}

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

const oppAlias = "o"

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

func applyDepartmentScope(q *gorm.DB, input opportunity.SearchByFilterInput) *gorm.DB {
	return applyDepartmentScopeRaw(q, input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
}

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
		return q.Where("1 = 0")
	}
	return q.Where("("+strings.Join(conds, " OR ")+")", args...)
}

func orderClause(sortField, sortOrder string) string {
	dir := "DESC"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "asc") {
		dir = "ASC"
	}
	switch strings.TrimSpace(sortField) {
	case "value":
		return fmt.Sprintf("%s.value_cents %s, %s.id ASC", oppAlias, dir, oppAlias)
	case "close_date":
		return fmt.Sprintf("%s.close_date %s NULLS LAST, %s.id ASC", oppAlias, dir, oppAlias)
	default:
		return fmt.Sprintf("%s.created_at %s, %s.id ASC", oppAlias, dir, oppAlias)
	}
}

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
