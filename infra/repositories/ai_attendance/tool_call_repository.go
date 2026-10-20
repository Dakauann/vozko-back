package ai_attendance_repository

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

const deleteToolCallsBeforeSQL = `DELETE FROM ai_tool_calls WHERE id IN (SELECT id FROM ai_tool_calls WHERE created_at < ? ORDER BY created_at LIMIT ?)`

type toolCallLog struct {
	db *gorm.DB
}

func NewToolCallLog(db *gorm.DB) aa.ToolCallLog {
	return &toolCallLog{db: db}
}

func (r *toolCallLog) Record(calls []aa.ToolCall) error {
	if len(calls) == 0 {
		return nil
	}
	rows := make([]schema.AIToolCallSchema, len(calls))
	for i, c := range calls {
		rows[i] = schema.AIToolCallSchema{
			ID:          c.ID,
			ToolCallID:  optionalText(c.ToolCallID),
			WorkspaceID: c.WorkspaceID,
			EntryID:     c.EntryID,
			EntryType:   c.EntryType,
			AgentID:     c.AgentID,
			SessionID:   c.SessionID,
			ToolName:    c.ToolName,
			Args:        encodedArgs(c.Args),
			Outcome:     string(c.Outcome),
			Error:       c.Error,
			ErrorCode:   c.ErrorCode,
			DurationMs:  c.DurationMs,
			RunID:       optionalText(c.RunID),
			CreatedAt:   c.CreatedAt,
		}
	}
	return r.db.Create(&rows).Error
}

const toolCallRunJoinSQL = `LEFT JOIN workflow_runs wr ON wr.id = ai_tool_calls.run_id AND wr.workspace_id = ai_tool_calls.workspace_id`

type toolCallWithWorkflow struct {
	schema.AIToolCallSchema
	RunWorkflowID *string
}

func (r *toolCallLog) ListByEntry(workspaceID, entryID, entryType string, pagination shared.Pagination) (*shared.PaginatedResult[*aa.ToolCall], error) {
	pagination = shared.NormalizePagination(pagination)
	var total int64
	if err := r.db.Model(&schema.AIToolCallSchema{}).Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType).Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []toolCallWithWorkflow
	err := r.db.Table("ai_tool_calls").
		Select("ai_tool_calls.*, wr.workflow_id AS run_workflow_id").
		Joins(toolCallRunJoinSQL).
		Where("ai_tool_calls.workspace_id = ? AND ai_tool_calls.entry_id = ? AND ai_tool_calls.entry_type = ?", workspaceID, entryID, entryType).
		Order("ai_tool_calls.created_at DESC").Limit(pagination.PageSize).Offset(pagination.Offset()).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]*aa.ToolCall, 0, len(rows))
	for i := range rows {
		call, err := toolCallFromRow(&rows[i].AIToolCallSchema)
		if err != nil {
			return nil, err
		}
		if rows[i].RunWorkflowID != nil && call.RunID != "" {
			call.WorkflowID = *rows[i].RunWorkflowID
		}
		items = append(items, call)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *toolCallLog) DeleteCreatedBefore(cutoff time.Time, limit int) (int64, error) {
	result := r.db.Exec(deleteToolCallsBeforeSQL, cutoff, limit)
	return result.RowsAffected, result.Error
}

func encodedArgs(args map[string]interface{}) *string {
	if len(args) == 0 {
		return nil
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	s := string(encoded)
	return &s
}

func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toolCallFromRow(row *schema.AIToolCallSchema) (*aa.ToolCall, error) {
	call := &aa.ToolCall{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		EntryID:     row.EntryID,
		EntryType:   row.EntryType,
		AgentID:     row.AgentID,
		SessionID:   row.SessionID,
		ToolName:    row.ToolName,
		Outcome:     aa.ToolCallOutcome(row.Outcome),
		Error:       row.Error,
		ErrorCode:   row.ErrorCode,
		DurationMs:  row.DurationMs,
		CreatedAt:   row.CreatedAt,
	}
	if row.RunID != nil {
		call.RunID = *row.RunID
	}
	if row.ToolCallID != nil {
		call.ToolCallID = *row.ToolCallID
	}
	if row.Args != nil && *row.Args != "" {
		if err := json.Unmarshal([]byte(*row.Args), &call.Args); err != nil {
			return nil, err
		}
	}
	return call, nil
}
