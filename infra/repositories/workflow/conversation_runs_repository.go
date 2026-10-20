package workflow_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const countConversationRunsSQL = `SELECT COUNT(*) FROM workflow_runs r
WHERE r.workspace_id = ? AND r.entry_id = ? AND r.entry_type = ?`

const conversationRunsSQL = `SELECT r.id, r.workflow_id, w.name AS workflow_name, r.workflow_version, r.entry_id, r.entry_type,
	r.status, r.trigger_type, r.started_by, r.started_at, r.completed_at, r.wait_reason, r.current_node_id,
	CASE WHEN r.status IN ('running', 'waiting') THEN ` + runCurrentNodeTypeSQL + ` END AS current_node_type
FROM workflow_runs r
` + runPinnedGraphJoinSQL + `
WHERE r.workspace_id = ? AND r.entry_id = ? AND r.entry_type = ?
ORDER BY r.started_at DESC
LIMIT ? OFFSET ?`

type ConversationRunsRepository struct {
	db *gorm.DB
}

func NewConversationRunsRepository(db *gorm.DB) *ConversationRunsRepository {
	return &ConversationRunsRepository{db: db}
}

var _ workflow.ConversationRuns = (*ConversationRunsRepository)(nil)

type conversationRunRow struct {
	ID              string
	WorkflowID      string
	WorkflowName    string
	WorkflowVersion *int
	EntryID         string
	EntryType       string
	Status          string
	TriggerType     string
	StartedBy       string
	StartedAt       time.Time
	CompletedAt     *time.Time
	WaitReason      string
	CurrentNodeID   string
	CurrentNodeType *string
}

func (r *ConversationRunsRepository) ListByEntry(workspaceID, entryID, entryType string, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.ConversationRun], error) {
	pagination = shared.NormalizePagination(pagination)
	var total int64
	if err := r.db.Raw(countConversationRunsSQL, workspaceID, entryID, entryType).Scan(&total).Error; err != nil {
		return nil, err
	}
	var rows []conversationRunRow
	if err := r.db.Raw(conversationRunsSQL, workspaceID, entryID, entryType, pagination.PageSize, pagination.Offset()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*workflow.ConversationRun, len(rows))
	for i, row := range rows {
		run := &workflow.ConversationRun{
			ID:              row.ID,
			WorkflowID:      row.WorkflowID,
			WorkflowName:    row.WorkflowName,
			WorkflowVersion: row.WorkflowVersion,
			EntryID:         row.EntryID,
			EntryType:       row.EntryType,
			Status:          workflow.RunStatus(row.Status),
			TriggerType:     workflow.TriggerType(row.TriggerType),
			StartedBy:       row.StartedBy,
			StartedAt:       row.StartedAt,
			CompletedAt:     row.CompletedAt,
			WaitReason:      workflow.WaitReason(row.WaitReason),
			CurrentNodeID:   row.CurrentNodeID,
		}
		if row.CurrentNodeType != nil {
			run.CurrentNodeType = workflow.NodeType(*row.CurrentNodeType)
		}
		items[i] = run
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}
