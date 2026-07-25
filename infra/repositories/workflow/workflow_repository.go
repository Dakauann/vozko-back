package workflow_repository

import (
	"encoding/json"
	"errors"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewWorkflowRepository(db *gorm.DB) workflow.WorkflowRepository {
	return &repository{db: db}
}

func (r *repository) Create(w *workflow.Workflow) error {
	graphJSON, err := w.GraphJSON()
	if err != nil {
		return err
	}
	triggerJSON, err := w.TriggerConfigJSON()
	if err != nil {
		return err
	}

	dbW := &schema.WorkflowSchema{
		ID:            w.ID,
		WorkspaceID:   w.WorkspaceID,
		DepartmentID:  nilIfEmpty(w.DepartmentID),
		Name:          w.Name,
		Description:   w.Description,
		Status:        string(w.Status),
		Type:          string(w.Type),
		TriggerType:   string(w.TriggerType),
		TriggerConfig: string(triggerJSON),
		Graph:         string(graphJSON),
		Version:       w.Version,
	}

	if err := r.db.Create(dbW).Error; err != nil {
		return err
	}

	w.ID = dbW.ID
	w.CreatedAt = dbW.CreatedAt
	w.UpdatedAt = dbW.UpdatedAt
	return nil
}

func (r *repository) Update(w *workflow.Workflow) error {
	graphJSON, err := w.GraphJSON()
	if err != nil {
		return err
	}
	triggerJSON, err := w.TriggerConfigJSON()
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"department_id":  nilIfEmpty(w.DepartmentID),
		"name":           w.Name,
		"description":    w.Description,
		"status":         string(w.Status),
		"type":           string(w.Type),
		"trigger_type":   string(w.TriggerType),
		"trigger_config": string(triggerJSON),
		"graph":          string(graphJSON),
		"version":        w.Version,
	}

	return r.db.Model(&schema.WorkflowSchema{}).Where("id = ?", w.ID).Updates(updates).Error
}

func (r *repository) Delete(workflowID string) error {
	return r.db.Delete(&schema.WorkflowSchema{}, "id = ?", workflowID).Error
}

func (r *repository) FindByID(workflowID string) (*workflow.Workflow, error) {
	var dbW schema.WorkflowSchema
	if err := r.db.Where("id = ?", workflowID).First(&dbW).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapWorkflowToDomain(&dbW)
}

// FindByIDs batch-loads workflows by id in one query. Used by inbox enrichment to
// resolve a workflow's name and its current-node type; kept off the domain interface
// (a narrow concrete method) since only the read model needs it.
func (r *repository) FindByIDs(ids []string) ([]*workflow.Workflow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var dbWs []schema.WorkflowSchema
	if err := r.db.Where("id IN ?", ids).Find(&dbWs).Error; err != nil {
		return nil, err
	}
	result := make([]*workflow.Workflow, 0, len(dbWs))
	for i := range dbWs {
		w, err := mapWorkflowToDomain(&dbWs[i])
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, nil
}

func (r *repository) FindByWorkspaceID(workspaceID string) ([]*workflow.Workflow, error) {
	var dbWs []schema.WorkflowSchema
	if err := r.db.Where("workspace_id = ?", workspaceID).Order("created_at DESC").Find(&dbWs).Error; err != nil {
		return nil, err
	}
	result := make([]*workflow.Workflow, 0, len(dbWs))
	for i := range dbWs {
		w, err := mapWorkflowToDomain(&dbWs[i])
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, nil
}

func (r *repository) List(input workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)
	query := r.db.Model(&schema.WorkflowSchema{}).Where("workspace_id = ?", input.WorkspaceID)

	if len(input.DepartmentIDs) > 0 {
		query = query.Where("department_id IN ?", input.DepartmentIDs)
	}

	if input.Search != "" {
		pattern := "%" + strings.ToLower(input.Search) + "%"
		query = query.Where("LOWER(name) LIKE ?", pattern)
	}
	if input.Status != nil {
		query = query.Where("status = ?", string(*input.Status))
	}
	if input.Type != nil {
		query = query.Where("type = ?", string(*input.Type))
	}

	var total int64
	if input.TriggerType == nil {
		if err := query.Count(&total).Error; err != nil {
			return nil, err
		}
	}

	var dbWs []schema.WorkflowSchema
	dbQuery := query.Order("created_at DESC")
	if input.TriggerType == nil {
		dbQuery = dbQuery.Offset(pagination.Offset()).Limit(pagination.PageSize)
	}
	if err := dbQuery.Find(&dbWs).Error; err != nil {
		return nil, err
	}

	items := make([]*workflow.Workflow, 0, len(dbWs))
	for i := range dbWs {
		w, err := mapWorkflowToDomain(&dbWs[i])
		if err != nil {
			return nil, err
		}
		if input.TriggerType != nil && !w.HasTriggerType(*input.TriggerType) {
			continue
		}
		items = append(items, w)
	}

	if input.TriggerType != nil {
		total = int64(len(items))
		start := pagination.Offset()
		if start > len(items) {
			start = len(items)
		}
		end := start + pagination.PageSize
		if end > len(items) {
			end = len(items)
		}
		items = items[start:end]
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) FindActiveByTrigger(workspaceID string, triggerType workflow.TriggerType) ([]*workflow.Workflow, error) {
	var dbWs []schema.WorkflowSchema

	if err := r.db.
		Where("workspace_id = ? AND status = ?", workspaceID, string(workflow.WorkflowStatusActive)).
		Find(&dbWs).Error; err != nil {
		return nil, err
	}
	result := make([]*workflow.Workflow, 0, len(dbWs))
	for i := range dbWs {
		w, err := mapWorkflowToDomain(&dbWs[i])
		if err != nil {
			return nil, err
		}
		if !w.HasTriggerType(triggerType) {
			continue
		}
		result = append(result, w)
	}
	return result, nil
}

func mapWorkflowToDomain(dbW *schema.WorkflowSchema) (*workflow.Workflow, error) {
	var graph workflow.Graph
	if dbW.Graph != "" {
		if err := json.Unmarshal([]byte(dbW.Graph), &graph); err != nil {
			return nil, err
		}
	}

	var triggerConfig map[string]interface{}
	if dbW.TriggerConfig != "" {
		if err := json.Unmarshal([]byte(dbW.TriggerConfig), &triggerConfig); err != nil {
			return nil, err
		}
	}
	if triggerConfig == nil {
		triggerConfig = make(map[string]interface{})
	}

	w := &workflow.Workflow{
		ID:            dbW.ID,
		WorkspaceID:   dbW.WorkspaceID,
		DepartmentID:  derefString(dbW.DepartmentID),
		Name:          dbW.Name,
		Description:   dbW.Description,
		Status:        workflow.WorkflowStatus(dbW.Status),
		Type:          workflow.WorkflowType(dbW.Type),
		TriggerType:   workflow.TriggerType(dbW.TriggerType),
		TriggerConfig: triggerConfig,
		Graph:         graph,
		Version:       dbW.Version,
		CreatedAt:     dbW.CreatedAt,
		UpdatedAt:     dbW.UpdatedAt,
	}

	if w.Type == "" {
		w.Type = w.TriggerType.WorkflowType()
	}
	return w, nil
}

func nilIfEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
