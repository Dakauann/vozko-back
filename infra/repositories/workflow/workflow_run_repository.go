package workflow_repository

import (
	"errors"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type runRepository struct {
	db *gorm.DB
}

func NewWorkflowRunRepository(db *gorm.DB) workflow.WorkflowRunRepository {
	return &runRepository{db: db}
}

func (r *runRepository) Create(run *workflow.WorkflowRun) error {
	stateJSON, err := run.State.JSON()
	if err != nil {
		return err
	}

	dbR := &schema.WorkflowRunSchema{
		ID:            run.ID,
		WorkflowID:    run.WorkflowID,
		WorkspaceID:   run.WorkspaceID,
		EntryID:       run.EntryID,
		EntryType:     run.EntryType,
		Status:        string(run.Status),
		TriggerNodeID: run.TriggerNodeID,
		CurrentNodeID: run.CurrentNodeID,
		State:         string(stateJSON),
		WakeAt:        run.WakeAt,
		WaitReason:    string(run.WaitReason),
		RetryCount:    run.RetryCount,
		Error:         run.Error,
		CompletedAt:   run.CompletedAt,
	}

	if err := r.db.Create(dbR).Error; err != nil {
		return err
	}

	run.ID = dbR.ID
	run.StartedAt = dbR.StartedAt
	run.UpdatedAt = dbR.UpdatedAt
	return nil
}

func (r *runRepository) Update(run *workflow.WorkflowRun) error {
	stateJSON, err := run.State.JSON()
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"status":          string(run.Status),
		"current_node_id": run.CurrentNodeID,
		"state":           string(stateJSON),
		"wake_at":         run.WakeAt,
		"wait_reason":     string(run.WaitReason),
		"retry_count":     run.RetryCount,
		"error":           run.Error,
		"completed_at":    run.CompletedAt,
	}

	return r.db.Model(&schema.WorkflowRunSchema{}).Where("id = ?", run.ID).Updates(updates).Error
}

func (r *runRepository) FindByID(runID string) (*workflow.WorkflowRun, error) {
	var dbR schema.WorkflowRunSchema
	if err := r.db.Where("id = ?", runID).First(&dbR).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapRunToDomain(&dbR)
}

func (r *runRepository) FindActiveByEntry(workflowID, entryID string) (*workflow.WorkflowRun, error) {
	var dbR schema.WorkflowRunSchema
	err := r.db.
		Where("workflow_id = ? AND entry_id = ? AND status IN ?", workflowID, entryID, []string{string(workflow.RunStatusRunning), string(workflow.RunStatusWaiting)}).
		First(&dbR).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapRunToDomain(&dbR)
}

func (r *runRepository) FindActiveByEntryAndTrigger(workflowID, entryID, triggerNodeID string) (*workflow.WorkflowRun, error) {
	var dbR schema.WorkflowRunSchema
	err := r.db.
		Where("workflow_id = ? AND entry_id = ? AND trigger_node_id = ? AND status IN ?", workflowID, entryID, triggerNodeID, []string{string(workflow.RunStatusRunning), string(workflow.RunStatusWaiting)}).
		First(&dbR).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapRunToDomain(&dbR)
}

// FindActiveByEntries batch-loads the active (running/waiting) run per entry for the
// given entry ids in one indexed query, keeping the most recently updated run when an
// entry has several. Returns a map keyed by entry id (entries with no active run are
// absent). Used by inbox enrichment; stays O(page) via the (entry_id) active index.
func (r *runRepository) FindActiveByEntries(entryIDs []string) (map[string]*workflow.WorkflowRun, error) {
	out := make(map[string]*workflow.WorkflowRun, len(entryIDs))
	if len(entryIDs) == 0 {
		return out, nil
	}
	var rows []schema.WorkflowRunSchema
	err := r.db.
		Where("entry_id IN ? AND status IN ?", entryIDs, []string{string(workflow.RunStatusRunning), string(workflow.RunStatusWaiting)}).
		Order("entry_id, updated_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		eid := rows[i].EntryID
		if _, seen := out[eid]; seen {
			continue // rows are ordered newest-first per entry
		}
		run, mErr := mapRunToDomain(&rows[i])
		if mErr != nil {
			return nil, mErr
		}
		out[eid] = run
	}
	return out, nil
}

func (r *runRepository) FindWaitingReplyByEntry(entryID string) (*workflow.WorkflowRun, error) {
	var dbR schema.WorkflowRunSchema
	err := r.db.
		Where("entry_id = ? AND status = ? AND wait_reason = ?", entryID, string(workflow.RunStatusWaiting), string(workflow.WaitReasonReply)).
		Order("updated_at DESC").
		First(&dbR).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapRunToDomain(&dbR)
}

func (r *runRepository) List(input workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)
	query := r.db.Model(&schema.WorkflowRunSchema{})

	if input.WorkspaceID != "" {
		query = query.Where("workspace_id = ?", input.WorkspaceID)
	}
	if input.WorkflowID != "" {
		query = query.Where("workflow_id = ?", input.WorkflowID)
	}
	if input.EntryID != "" {
		query = query.Where("entry_id = ?", input.EntryID)
	}
	if input.Status != nil {
		query = query.Where("status = ?", string(*input.Status))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbRs []schema.WorkflowRunSchema
	if err := query.
		Order("started_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbRs).Error; err != nil {
		return nil, err
	}

	items := make([]*workflow.WorkflowRun, 0, len(dbRs))
	for i := range dbRs {
		run, err := mapRunToDomain(&dbRs[i])
		if err != nil {
			return nil, err
		}
		items = append(items, run)
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *runRepository) FindWakeableRuns(now int64, limit int) ([]*workflow.WorkflowRun, error) {
	var dbRs []schema.WorkflowRunSchema
	err := r.db.
		Where("status = ? AND wake_at IS NOT NULL AND wake_at <= ?", string(workflow.RunStatusWaiting), now).
		Limit(limit).
		Find(&dbRs).Error
	if err != nil {
		return nil, err
	}
	return mapRunSlice(dbRs)
}

func (r *runRepository) FindStuckRuns(cutoff int64) ([]*workflow.WorkflowRun, error) {
	var dbRs []schema.WorkflowRunSchema
	nowMs := time.Now().UTC().UnixMilli()
	err := r.db.
		Where("status IN ? AND updated_at < to_timestamp(?) AND NOT (status = ? AND wake_at IS NOT NULL AND wake_at > ?)",
			[]string{string(workflow.RunStatusRunning), string(workflow.RunStatusWaiting)},
			cutoff,
			string(workflow.RunStatusWaiting), nowMs,
		).
		Find(&dbRs).Error
	if err != nil {
		return nil, err
	}
	return mapRunSlice(dbRs)
}

func (r *runRepository) CancelByWorkflow(workflowID string) (int64, error) {
	result := r.db.Model(&schema.WorkflowRunSchema{}).
		Where("workflow_id = ? AND status IN ?", workflowID, []string{string(workflow.RunStatusRunning), string(workflow.RunStatusWaiting)}).
		Updates(map[string]interface{}{"status": string(workflow.RunStatusCancelled)})
	return result.RowsAffected, result.Error
}

func (r *runRepository) CountByWorkflow(workflowID string) (int64, error) {
	var count int64
	err := r.db.Model(&schema.WorkflowRunSchema{}).Where("workflow_id = ?", workflowID).Count(&count).Error
	return count, err
}

func (r *runRepository) CountActiveByWorkspace(workspaceID string) (int64, error) {
	var count int64
	err := r.db.Model(&schema.WorkflowRunSchema{}).
		Where("workspace_id = ? AND status IN ?", workspaceID, []string{string(workflow.RunStatusRunning), string(workflow.RunStatusWaiting)}).
		Count(&count).Error
	return count, err
}

func mapRunToDomain(dbR *schema.WorkflowRunSchema) (*workflow.WorkflowRun, error) {
	state, err := workflow.RunStateFromJSON([]byte(dbR.State))
	if err != nil {
		return nil, err
	}

	return &workflow.WorkflowRun{
		ID:            dbR.ID,
		WorkflowID:    dbR.WorkflowID,
		WorkspaceID:   dbR.WorkspaceID,
		EntryID:       dbR.EntryID,
		EntryType:     dbR.EntryType,
		Status:        workflow.RunStatus(dbR.Status),
		TriggerNodeID: dbR.TriggerNodeID,
		CurrentNodeID: dbR.CurrentNodeID,
		State:         state,
		WakeAt:        dbR.WakeAt,
		WaitReason:    workflow.WaitReason(dbR.WaitReason),
		RetryCount:    dbR.RetryCount,
		Error:         dbR.Error,
		StartedAt:     dbR.StartedAt,
		UpdatedAt:     dbR.UpdatedAt,
		CompletedAt:   dbR.CompletedAt,
	}, nil
}

func mapRunSlice(dbRs []schema.WorkflowRunSchema) ([]*workflow.WorkflowRun, error) {
	result := make([]*workflow.WorkflowRun, 0, len(dbRs))
	for i := range dbRs {
		run, err := mapRunToDomain(&dbRs[i])
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, nil
}
