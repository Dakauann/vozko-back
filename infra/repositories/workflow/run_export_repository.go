package workflow_repository

import (
	"context"
	"encoding/json"
	"errors"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

var errExportUnscoped = errors.New("workflow: a run export needs its workspace and workflow")

func NewRunExportSource(db *gorm.DB) workflow.RunExportSource {
	return &runRepository{db: db}
}

func NewRunStepsReader(db *gorm.DB) workflow.RunStepsReader {
	return &logRepository{db: db}
}

func exportedRuns(tx *gorm.DB, filter workflow.RunExportFilter) *gorm.DB {
	return filteredRuns(tx.Model(&schema.WorkflowRunSchema{}), filter.Runs).
		Where("started_at >= ?", filter.From).
		Where("started_at < ?", filter.To)
}

func exportPageQuery(tx *gorm.DB, filter workflow.RunExportFilter, after *workflow.RunCursor, limit int) *gorm.DB {
	query := exportedRuns(tx, filter)
	if after != nil {
		query = query.Where("started_at <= ? AND (started_at < ? OR id < ?)", after.StartedAt, after.StartedAt, after.ID)
	}
	return query.Order("started_at DESC, id DESC").Limit(limit)
}

func requireExportScope(filter workflow.RunExportFilter) error {
	if filter.Runs.WorkspaceID == "" || filter.Runs.WorkflowID == "" {
		return errExportUnscoped
	}
	return nil
}

func (r *runRepository) CountForExportUpTo(ctx context.Context, filter workflow.RunExportFilter, ceiling int) (int64, error) {
	if err := requireExportScope(filter); err != nil {
		return 0, err
	}
	tx := r.db.WithContext(ctx)
	capped := exportedRuns(tx, filter).Select("1").Limit(ceiling)
	var total int64
	if err := tx.Table("(?) AS capped", capped).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *runRepository) ExportPage(ctx context.Context, filter workflow.RunExportFilter, after *workflow.RunCursor, limit int) ([]*workflow.WorkflowRun, error) {
	if err := requireExportScope(filter); err != nil {
		return nil, err
	}
	var rows []schema.WorkflowRunSchema
	if err := exportPageQuery(r.db.WithContext(ctx), filter, after, limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapRunSlice(rows)
}

const exportStepsSQL = `SELECT l.* FROM jsonb_array_elements_text(CAST(? AS jsonb)) WITH ORDINALITY AS p(run_id, position)
JOIN workflow_run_logs l ON l.run_id = CAST(p.run_id AS uuid)
ORDER BY p.position, l.executed_at, l.id`

func (r *logRepository) EachRunSteps(ctx context.Context, runIDs []string, visit func(runID string, steps []*workflow.WorkflowRunLog) error) error {
	if len(runIDs) == 0 {
		return nil
	}
	ids, err := json.Marshal(runIDs)
	if err != nil {
		return err
	}
	tx := r.db.WithContext(ctx)
	rows, err := tx.Raw(exportStepsSQL, string(ids)).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	var current string
	var group []*workflow.WorkflowRunLog
	for rows.Next() {
		var row schema.WorkflowRunLogSchema
		if err := tx.ScanRows(rows, &row); err != nil {
			return err
		}
		step, err := mapLogToDomain(&row)
		if err != nil {
			return err
		}
		if len(group) > 0 && step.RunID != current {
			if err := visit(current, group); err != nil {
				return err
			}
			group = nil
		}
		current = step.RunID
		group = append(group, step)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(group) > 0 {
		return visit(current, group)
	}
	return nil
}
