package workflow_repository

import (
	"database/sql"
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

const latestTriggerDataWindowSQL = `SELECT workflow_runs.id, workflow_runs.started_at, workflow_runs.trigger_data, workflow_runs.entry_id, workflow_runs.entry_type
  FROM workflow_runs
  WHERE workflow_runs.workspace_id = ? AND workflow_runs.workflow_id = ? AND workflow_runs.trigger_type = ? AND workflow_runs.trigger_data IS NOT NULL
  ORDER BY workflow_runs.started_at DESC
  LIMIT ?`

func latestTriggerDataSQL(visible string) string {
	where := ""
	if visible != "" {
		where = "\nWHERE " + visible
	}
	return "SELECT workflow_runs.id, workflow_runs.started_at, workflow_runs.trigger_data\nFROM (" + latestTriggerDataWindowSQL + ") AS workflow_runs" +
		where + "\nORDER BY workflow_runs.started_at DESC\nLIMIT 1"
}

type triggerInputHistory struct {
	db *gorm.DB
}

func NewTriggerInputHistory(db *gorm.DB) workflow.TriggerInputHistory {
	return &triggerInputHistory{db: db}
}

type recordedTriggerRow struct {
	ID          string
	StartedAt   time.Time
	TriggerData sql.NullString
}

func (h *triggerInputHistory) LatestTriggerData(workspaceID, workflowID string, trigger workflow.TriggerType, visibility conversation.VisibilityScope) (*workflow.RecordedTriggerData, error) {
	args := []interface{}{workspaceID, workflowID, string(trigger), workflow.TriggerInputHistoryWindow}
	visible, visibleArgs := visibleRunEntries(workspaceID, visibility)
	args = append(args, visibleArgs...)
	var rows []recordedTriggerRow
	if err := h.db.Raw(latestTriggerDataSQL(visible), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	var data map[string]interface{}
	if err := decodeColumn(&row.TriggerData.String, &data); err != nil {
		return nil, err
	}
	return &workflow.RecordedTriggerData{RunID: row.ID, StartedAt: row.StartedAt, Data: data}, nil
}
