package workflow_repository

import (
	"encoding/json"
	"fmt"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type logRepository struct {
	db *gorm.DB
}

func NewWorkflowRunLogRepository(db *gorm.DB) workflow.WorkflowRunLogRepository {
	return &logRepository{db: db}
}

func (r *logRepository) Create(logEntry *workflow.WorkflowRunLog) error {
	attempt := logEntry.Attempt
	duration := logEntry.DurationMs
	var columns jsonColumns
	dbL := &schema.WorkflowRunLogSchema{
		ID:         logEntry.ID,
		RunID:      logEntry.RunID,
		NodeID:     logEntry.NodeID,
		NodeType:   string(logEntry.NodeType),
		Status:     string(logEntry.Status),
		Input:      columns.of(logEntry.Input, logEntry.Input != nil),
		Output:     columns.of(logEntry.Output, logEntry.Output != nil),
		Error:      logEntry.Error,
		Attempt:    &attempt,
		StartedAt:  logEntry.StartedAt,
		FinishedAt: logEntry.FinishedAt,
		DurationMs: &duration,
		Branch:     logEntry.Branch,
		ErrorKind:  string(logEntry.ErrorKind),
		ErrorChain: columns.of(logEntry.ErrorChain, len(logEntry.ErrorChain) > 0),
		Stack:      nilIfEmpty(logEntry.Stack),
		ScriptLogs: columns.of(logEntry.ScriptLogs, len(logEntry.ScriptLogs) > 0),
		HTTP:       columns.of(logEntry.HTTP, len(logEntry.HTTP) > 0),
	}
	if columns.err != nil {
		return columns.err
	}

	if err := r.db.Create(dbL).Error; err != nil {
		return err
	}

	logEntry.ID = dbL.ID
	logEntry.ExecutedAt = dbL.ExecutedAt
	return nil
}

type jsonColumns struct {
	err error
}

func (c *jsonColumns) of(v interface{}, present bool) *string {
	if !present || c.err != nil {
		return nil
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		c.err = fmt.Errorf("encode a json column: %w", err)
		return nil
	}
	s := string(encoded)
	return &s
}

func (r *logRepository) FindByRunID(runID string) ([]*workflow.WorkflowRunLog, error) {
	var dbLs []schema.WorkflowRunLogSchema
	if err := r.db.Where("run_id = ?", runID).Order("executed_at ASC").Find(&dbLs).Error; err != nil {
		return nil, err
	}
	return mapLogsToDomain(dbLs)
}

func (r *logRepository) PageByRunID(runID string, offset, limit int) ([]*workflow.WorkflowRunLog, error) {
	var dbLs []schema.WorkflowRunLogSchema
	if err := r.db.Where("run_id = ?", runID).Order("executed_at ASC, id ASC").Limit(limit).Offset(offset).Find(&dbLs).Error; err != nil {
		return nil, err
	}
	return mapLogsToDomain(dbLs)
}

func mapLogsToDomain(dbLs []schema.WorkflowRunLogSchema) ([]*workflow.WorkflowRunLog, error) {
	result := make([]*workflow.WorkflowRunLog, 0, len(dbLs))
	for i := range dbLs {
		l, err := mapLogToDomain(&dbLs[i])
		if err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, nil
}

const latestPinnableStepsSQL = `SELECT s.* FROM jsonb_array_elements_text(CAST(? AS jsonb)) AS n(node_id)
CROSS JOIN LATERAL (
	SELECT * FROM workflow_run_logs
	WHERE run_id = ? AND node_id = n.node_id AND status IN ?
	ORDER BY executed_at DESC, id DESC
	LIMIT 1
) s`

const stepOfRunSQL = `SELECT * FROM workflow_run_logs WHERE id = ? AND run_id = ?`

const firstTriggerStepSQL = `SELECT * FROM workflow_run_logs
WHERE run_id = ? AND node_type IN ? AND output IS NOT NULL
ORDER BY executed_at, id
LIMIT 1`

var _ workflow.RunStepPicker = (*logRepository)(nil)

func (r *logRepository) PickSteps(runID string, pick workflow.StepPick) ([]*workflow.WorkflowRunLog, error) {
	var picked []schema.WorkflowRunLogSchema
	read := func(sql string, args ...interface{}) error {
		var rows []schema.WorkflowRunLogSchema
		if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
			return err
		}
		picked = append(picked, rows...)
		return nil
	}
	if stepIDs := uuidsOf([]string{pick.StepID}); len(stepIDs) == 1 {
		if err := read(stepOfRunSQL, stepIDs[0], runID); err != nil {
			return nil, err
		}
	}
	if len(pick.LatestOfNodes) > 0 {
		nodes, err := json.Marshal(pick.LatestOfNodes)
		if err != nil {
			return nil, err
		}
		if err := read(latestPinnableStepsSQL, string(nodes), runID, workflow.PinnableStepStatuses()); err != nil {
			return nil, err
		}
	}
	if pick.Trigger {
		if err := read(firstTriggerStepSQL, runID, workflow.TriggerNodeTypes()); err != nil {
			return nil, err
		}
	}
	return mapLogsToDomain(picked)
}

func (r *logRepository) CountByRun(runID string) (int64, error) {
	var count int64
	err := r.db.Model(&schema.WorkflowRunLogSchema{}).Where("run_id = ?", runID).Count(&count).Error
	return count, err
}

func decodeColumn(column *string, into interface{}) error {
	if column == nil || *column == "" {
		return nil
	}
	return json.Unmarshal([]byte(*column), into)
}

func mapLogToDomain(dbL *schema.WorkflowRunLogSchema) (*workflow.WorkflowRunLog, error) {
	l := &workflow.WorkflowRunLog{
		ID:         dbL.ID,
		RunID:      dbL.RunID,
		NodeID:     dbL.NodeID,
		NodeType:   workflow.NodeType(dbL.NodeType),
		Status:     workflow.LogStatus(dbL.Status),
		Error:      dbL.Error,
		ExecutedAt: dbL.ExecutedAt,
		StartedAt:  dbL.StartedAt,
		FinishedAt: dbL.FinishedAt,
		Branch:     dbL.Branch,
		ErrorKind:  workflow.ErrorKind(dbL.ErrorKind),
	}
	if dbL.Attempt != nil {
		l.Attempt = *dbL.Attempt
	}
	if dbL.DurationMs != nil {
		l.DurationMs = *dbL.DurationMs
	}
	if dbL.Stack != nil {
		l.Stack = *dbL.Stack
	}
	for _, column := range []struct {
		value *string
		into  interface{}
	}{
		{dbL.Input, &l.Input},
		{dbL.Output, &l.Output},
		{dbL.ErrorChain, &l.ErrorChain},
		{dbL.ScriptLogs, &l.ScriptLogs},
		{dbL.HTTP, &l.HTTP},
	} {
		if err := decodeColumn(column.value, column.into); err != nil {
			return nil, err
		}
	}
	return l, nil
}
