package workflow_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/workflow"
)

const pruneStepPayloadsSQL = `UPDATE workflow_run_logs SET input = NULL, output = NULL, http = NULL, script_logs = NULL, stack = NULL
WHERE id IN (
	SELECT id FROM workflow_run_logs
	WHERE executed_at < ?
	  AND (input IS NOT NULL OR output IS NOT NULL OR http IS NOT NULL OR script_logs IS NOT NULL OR stack IS NOT NULL)
	LIMIT ?
)`

const pruneTriggerDataSQL = `UPDATE workflow_runs SET trigger_data = NULL
WHERE id IN (
	SELECT id FROM workflow_runs
	WHERE trigger_data IS NOT NULL AND started_at < ?
	LIMIT ?
)`

const deleteStepsOfFinishedRunsSQL = `DELETE FROM workflow_run_logs
WHERE id IN (
	SELECT l.id FROM workflow_run_logs l
	JOIN workflow_runs r ON r.id = l.run_id
	WHERE r.status IN ('completed', 'error', 'cancelled')
	  AND r.started_at < ?
	LIMIT ?
)`

const deleteFinishedRunsSQL = `DELETE FROM workflow_runs
WHERE id IN (
	SELECT r.id FROM workflow_runs r
	WHERE r.status IN ('completed', 'error', 'cancelled')
	  AND r.started_at < ?
	  AND NOT EXISTS (SELECT 1 FROM workflow_run_logs l WHERE l.run_id = r.id)
	LIMIT ?
)`

const pruneWorkflowVersionsSQL = `DELETE FROM workflow_versions
WHERE id IN (
	SELECT v.id FROM workflow_versions v
	LEFT JOIN workflows w ON w.id = v.workflow_id
	WHERE (
		w.id IS NULL OR w.deleted_at IS NOT NULL
		OR (
			v.workflow_id IN (SELECT c.workflow_id FROM workflow_versions c GROUP BY c.workflow_id HAVING COUNT(*) > ?)
			AND v.version IS DISTINCT FROM w.published_version
			AND v.version <= (
				SELECT k.version FROM workflow_versions k
				WHERE k.workflow_id = v.workflow_id
				ORDER BY k.version DESC
				OFFSET ? LIMIT 1
			)
		)
	)
	AND NOT EXISTS (
		SELECT 1 FROM workflow_runs r
		WHERE r.workflow_id = v.workflow_id AND r.workflow_version = v.version
		  AND r.status IN ('running', 'waiting')
	)
	LIMIT ?
)`

type traceRetentionRepository struct {
	db *gorm.DB
}

func NewTraceRetentionRepository(db *gorm.DB) workflow.TraceRetentionRepository {
	return &traceRetentionRepository{db: db}
}

func (r *traceRetentionRepository) PruneStepPayloads(executedBefore time.Time, limit int) (int64, error) {
	return r.exec(pruneStepPayloadsSQL, executedBefore, limit)
}

func (r *traceRetentionRepository) PruneTriggerData(startedBefore time.Time, limit int) (int64, error) {
	return r.exec(pruneTriggerDataSQL, startedBefore, limit)
}

func (r *traceRetentionRepository) DeleteStepsOfFinishedRuns(startedBefore time.Time, limit int) (int64, error) {
	return r.exec(deleteStepsOfFinishedRunsSQL, startedBefore, limit)
}

func (r *traceRetentionRepository) DeleteFinishedRuns(startedBefore time.Time, limit int) (int64, error) {
	return r.exec(deleteFinishedRunsSQL, startedBefore, limit)
}

func (r *traceRetentionRepository) PruneVersions(keepLatest, limit int) (int64, error) {
	result := r.db.Exec(pruneWorkflowVersionsSQL, keepLatest, keepLatest, limit)
	return result.RowsAffected, result.Error
}

func (r *traceRetentionRepository) exec(statement string, before time.Time, limit int) (int64, error) {
	result := r.db.Exec(statement, before, limit)
	return result.RowsAffected, result.Error
}
