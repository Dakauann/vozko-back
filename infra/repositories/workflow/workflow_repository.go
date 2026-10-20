package workflow_repository

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db       *gorm.DB
	versions *versionGraphs
}

func NewWorkflowRepository(db *gorm.DB) workflow.WorkflowRepository {
	return &repository{db: db, versions: newVersionGraphs()}
}

type workflowRow struct {
	schema.WorkflowSchema
	PublishedAt        *time.Time
	PublishedGraphHash *string
}

func (r *repository) withPublication() *gorm.DB {
	return r.db.Model(&schema.WorkflowSchema{}).Select(workflowWithPublicationSelect)
}

func mapRowToDomain(row *workflowRow) (*workflow.Workflow, error) {
	w, err := mapWorkflowToDomain(&row.WorkflowSchema)
	if err != nil {
		return nil, err
	}
	w.PublishedAt = row.PublishedAt
	w.PublishedGraphHash = derefString(row.PublishedGraphHash)
	return w, nil
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

const saveDraftSQL = `UPDATE workflows
SET name = ?, description = ?, type = ?, trigger_type = ?, trigger_config = ?, graph = ?, version = version + 1, updated_at = ?
WHERE id = ? AND version = ? AND deleted_at IS NULL
RETURNING version`

const draftRevisionSQL = `SELECT version FROM workflows WHERE id = ? AND deleted_at IS NULL`

func (r *repository) SaveDraft(w *workflow.Workflow, expectedRevision int) (int, error) {
	if err := workflow.RequireDraftRevision(expectedRevision); err != nil {
		return 0, err
	}
	graphJSON, err := w.GraphJSON()
	if err != nil {
		return 0, err
	}
	triggerJSON, err := w.TriggerConfigJSON()
	if err != nil {
		return 0, err
	}
	var saved []int
	if err := r.db.Raw(saveDraftSQL, w.Name, w.Description, string(w.Type), string(w.TriggerType), string(triggerJSON), string(graphJSON), time.Now().UTC(), w.ID, expectedRevision).
		Scan(&saved).Error; err != nil {
		return 0, err
	}
	if len(saved) == 1 {
		return saved[0], nil
	}
	var current []int
	if err := r.db.Raw(draftRevisionSQL, w.ID).Scan(&current).Error; err != nil {
		return 0, err
	}
	if len(current) == 0 {
		return 0, workflow.ErrWorkflowNotFound
	}
	return 0, &workflow.DraftConflict{WorkflowID: w.ID, Expected: expectedRevision, Current: current[0]}
}

func (r *repository) UpdateDepartment(workflowID, departmentID string) error {
	res := r.db.Model(&schema.WorkflowSchema{}).Where("id = ?", workflowID).Update("department_id", nilIfEmpty(departmentID))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return workflow.ErrWorkflowNotFound
	}
	return nil
}

const deleteVersionsOfWorkflowSQL = `DELETE FROM workflow_versions v
WHERE v.workflow_id = ?
  AND NOT EXISTS (
	SELECT 1 FROM workflow_runs r
	WHERE r.workflow_id = v.workflow_id AND r.workflow_version = v.version
	  AND r.status IN ('running', 'waiting')
  )`

func (r *repository) Delete(workflowID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&schema.WorkflowSchema{}, "id = ?", workflowID).Error; err != nil {
			return err
		}
		return tx.Exec(deleteVersionsOfWorkflowSQL, workflowID).Error
	})
}

func (r *repository) FindByID(workflowID string) (*workflow.Workflow, error) {
	var rows []workflowRow
	if err := r.withPublication().Where("workflows.id = ?", workflowID).Limit(1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].ID == "" {
		return nil, nil
	}
	return mapRowToDomain(&rows[0])
}

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
	if input.EntryKind != nil {
		filter, args := entryKindFilter(input.WorkspaceID, *input.EntryKind)
		query = query.Where(filter, args...)
	}

	var total int64
	if input.TriggerType == nil {
		if err := query.Count(&total).Error; err != nil {
			return nil, err
		}
	}

	var rows []workflowRow
	dbQuery := query.Select(workflowWithPublicationSelect).Order("created_at DESC")
	if input.TriggerType == nil {
		dbQuery = dbQuery.Offset(pagination.Offset()).Limit(pagination.PageSize)
	}
	if err := dbQuery.Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*workflow.Workflow, 0, len(rows))
	for i := range rows {
		w, err := mapRowToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		if input.TriggerType != nil {
			has, err := r.listedTrigger(w, input, *input.TriggerType)
			if err != nil {
				return nil, err
			}
			if !has {
				continue
			}
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

func (r *repository) listedTrigger(w *workflow.Workflow, input workflow.ListWorkflowsInput, triggerType workflow.TriggerType) (bool, error) {
	if input.Status == nil || *input.Status != workflow.WorkflowStatusActive {
		return w.HasTriggerType(triggerType), nil
	}
	running, err := workflow.PublishedOf(r, w)
	if errors.Is(err, workflow.ErrVersionNotFound) {
		slog.Warn("workflow.list.published_version_missing", slog.String("workspace_id", w.WorkspaceID), slog.String("workflow_id", w.ID))
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return running != nil && running.HasTriggerType(triggerType), nil
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

		PublishedVersion: dbW.PublishedVersion,
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
