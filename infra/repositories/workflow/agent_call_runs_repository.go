package workflow_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/workflow"
)

const runGraphSQL = `(CASE WHEN COALESCE(r.workflow_version, w.published_version) IS NULL THEN w.graph ELSE v.graph END)`

const runCurrentNodeTypeSQL = `CASE WHEN jsonb_typeof(` + runGraphSQL + `->'nodes') = 'array' THEN (
		SELECT n->>'type' FROM jsonb_array_elements(` + runGraphSQL + `->'nodes') n WHERE n->>'id' = r.current_node_id LIMIT 1
	) END`

const runPinnedGraphJoinSQL = `JOIN workflows w ON w.id = r.workflow_id
LEFT JOIN workflow_versions v ON v.workflow_id = r.workflow_id AND v.version = COALESCE(r.workflow_version, w.published_version)`

const latestAgentCallRunSQL = `SELECT r.id, r.workflow_id, w.name AS workflow_name, r.status, r.current_node_id,
	` + runCurrentNodeTypeSQL + ` AS current_node_type,
	r.wait_reason, r.wake_at, r.agent_result, r.error, r.started_at, r.completed_at,
	CASE WHEN r.status = 'waiting' AND r.wait_reason IN ('reply', 'form') THEN (
		SELECT m.text FROM conversation_messages m
		WHERE m.entry_id = r.entry_id AND m.entry_type = r.entry_type
		  AND m.sender_kind = 'workflow' AND m.sender_id = 'workflow:' || r.workflow_id::text
		  AND m.created_at >= r.started_at AND m.deleted_at IS NULL
		ORDER BY m.created_at DESC LIMIT 1
	) END AS last_question
FROM workflow_runs r
` + runPinnedGraphJoinSQL + `
WHERE r.workspace_id = ? AND r.entry_id = ? AND r.entry_type = ?
  AND r.started_by LIKE 'agent:%'
  AND NOT EXISTS (
	SELECT 1 FROM conversation_events e
	WHERE e.entry_id = r.entry_id AND e.entry_type = r.entry_type
	  AND e.event_type = 'finished' AND e.created_at > r.started_at
  )
ORDER BY r.started_at DESC
LIMIT 1`

const cancelActiveAgentRunsSQL = `UPDATE workflow_runs SET status = 'cancelled', error = ?, completed_at = now(), updated_at = now()
WHERE entry_id = ? AND entry_type = ?
  AND status IN ('running', 'waiting')
  AND started_by LIKE 'agent:%'
RETURNING id, workspace_id, workflow_id, current_node_id`

const claimAgentContinuationSQL = `UPDATE workflow_runs SET agent_continued_at = now(), agent_continuation_attempts = agent_continuation_attempts + 1
WHERE id = ? AND agent_continued_at IS NULL AND started_by LIKE 'agent:%'`

const releaseAgentContinuationSQL = `UPDATE workflow_runs SET agent_continued_at = NULL
WHERE id = ? AND agent_continued_at IS NOT NULL AND agent_continuation_attempts < ?`

const agentRunsAwaitingContinuationSQL = `SELECT id, workflow_id, workspace_id, entry_id, entry_type, status, started_by, agent_result, updated_at
FROM workflow_runs
WHERE started_by LIKE 'agent:%' AND agent_continued_at IS NULL
  AND status IN ('completed', 'error')
  AND updated_at >= ? AND updated_at < ?
ORDER BY updated_at
LIMIT ?`

type AgentCallRunsRepository struct {
	db *gorm.DB
}

func NewAgentCallRunsRepository(db *gorm.DB) *AgentCallRunsRepository {
	return &AgentCallRunsRepository{db: db}
}

type agentCallRunRow struct {
	ID              string
	WorkflowID      string
	WorkflowName    string
	Status          string
	CurrentNodeID   string
	CurrentNodeType *string
	WaitReason      string
	WakeAt          *int64
	AgentResult     *string
	Error           string
	StartedAt       time.Time
	CompletedAt     *time.Time
	LastQuestion    *string
}

func (r *AgentCallRunsRepository) LatestForEntry(workspaceID, entryID, entryType string) (*workflow.AgentCallRun, error) {
	var rows []agentCallRunRow
	if err := r.db.Raw(latestAgentCallRunSQL, workspaceID, entryID, entryType).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].ID == "" {
		return nil, nil
	}
	row := rows[0]
	var agentResult map[string]interface{}
	if err := decodeColumn(row.AgentResult, &agentResult); err != nil {
		return nil, err
	}
	return &workflow.AgentCallRun{
		RunID:           row.ID,
		WorkflowID:      row.WorkflowID,
		WorkflowName:    row.WorkflowName,
		Status:          workflow.RunStatus(row.Status),
		CurrentNodeID:   row.CurrentNodeID,
		CurrentNodeType: workflow.NodeType(derefString(row.CurrentNodeType)),
		WaitReason:      workflow.WaitReason(row.WaitReason),
		WakeAt:          row.WakeAt,
		LastQuestion:    derefString(row.LastQuestion),
		AgentResult:     agentResult,
		Error:           row.Error,
		StartedAt:       row.StartedAt,
		FinishedAt:      row.CompletedAt,
	}, nil
}

func (r *AgentCallRunsRepository) CancelActiveAgentRuns(entryID, entryType, reason string) ([]workflow.CancelledAgentRun, error) {
	var rows []struct {
		ID            string
		WorkspaceID   string
		WorkflowID    string
		CurrentNodeID string
	}
	if err := r.db.Raw(cancelActiveAgentRunsSQL, reason, entryID, entryType).Scan(&rows).Error; err != nil {
		return nil, err
	}
	cancelled := make([]workflow.CancelledAgentRun, 0, len(rows))
	for _, row := range rows {
		cancelled = append(cancelled, workflow.CancelledAgentRun{
			RunID: row.ID, WorkspaceID: row.WorkspaceID, WorkflowID: row.WorkflowID, CurrentNodeID: row.CurrentNodeID,
		})
	}
	return cancelled, nil
}

func (r *AgentCallRunsRepository) ClaimAgentContinuation(runID string) (bool, error) {
	res := r.db.Exec(claimAgentContinuationSQL, runID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *AgentCallRunsRepository) ReleaseAgentContinuation(runID string) (bool, error) {
	res := r.db.Exec(releaseAgentContinuationSQL, runID, workflow.MaxAgentContinuationAttempts)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *AgentCallRunsRepository) FinishedWithoutContinuation(finishedFrom, finishedBefore time.Time, limit int) ([]*workflow.WorkflowRun, error) {
	var rows []struct {
		ID          string
		WorkflowID  string
		WorkspaceID string
		EntryID     string
		EntryType   string
		Status      string
		StartedBy   string
		AgentResult *string
		UpdatedAt   time.Time
	}
	if err := r.db.Raw(agentRunsAwaitingContinuationSQL, finishedFrom, finishedBefore, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	runs := make([]*workflow.WorkflowRun, 0, len(rows))
	for _, row := range rows {
		var agentResult map[string]interface{}
		if err := decodeColumn(row.AgentResult, &agentResult); err != nil {
			return nil, err
		}
		runs = append(runs, &workflow.WorkflowRun{
			ID: row.ID, WorkflowID: row.WorkflowID, WorkspaceID: row.WorkspaceID, EntryID: row.EntryID, EntryType: row.EntryType,
			Status: workflow.RunStatus(row.Status), StartedBy: row.StartedBy, AgentResult: agentResult, UpdatedAt: row.UpdatedAt,
		})
	}
	return runs, nil
}

var (
	_ workflow.AgentCallRuns            = (*AgentCallRunsRepository)(nil)
	_ workflow.AgentRunCanceller        = (*AgentCallRunsRepository)(nil)
	_ workflow.AgentContinuationClaims  = (*AgentCallRunsRepository)(nil)
	_ workflow.AgentContinuationBacklog = (*AgentCallRunsRepository)(nil)
)
