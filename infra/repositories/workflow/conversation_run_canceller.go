package workflow_repository

import (
	"context"
	"gorm.io/gorm"
	"vozko/domain/workflow"
)

type conversationRunCanceller struct{ db *gorm.DB }

func NewConversationRunCanceller(db *gorm.DB) workflow.ConversationRunCanceller {
	return &conversationRunCanceller{db: db}
}

const cancelConversationRunsSQL = `UPDATE workflow_runs SET status = 'cancelled', error = ?, completed_at = NOW(), updated_at = NOW(), wake_at = NULL, wait_reason = '', waiting_since = NULL
 WHERE workspace_id = ? AND entry_id = ? AND entry_type = ? AND entry_type NOT IN ('lead', 'sip_call') AND status IN ('running','waiting') AND trigger_type IN (?, ?) AND started_by NOT LIKE ?
 RETURNING id AS run_id, workspace_id, workflow_id, current_node_id`

func (r *conversationRunCanceller) CancelConversationRuns(ctx context.Context, workspaceID, entryID, entryType, reason string) ([]workflow.CancelledRun, error) {
	var cancelled []workflow.CancelledRun
	err := r.db.WithContext(ctx).Raw(cancelConversationRunsSQL, reason, workspaceID, entryID, entryType, string(workflow.TriggerMessageReceived), string(workflow.TriggerFirstMessage), workflow.StartedByAgent("")+"%").Scan(&cancelled).Error
	return cancelled, err
}
