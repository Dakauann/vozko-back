package conversation_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
)

type ChannelConversationTable struct {
	DB              *gorm.DB
	NewModel        func() any
	ContainerColumn string
	NotFound        error
}

func (t ChannelConversationTable) model(ctx context.Context) *gorm.DB {
	return t.DB.WithContext(ctx).Model(t.NewModel())
}

func (t ChannelConversationTable) affected(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return t.NotFound
	}
	return nil
}

func (t ChannelConversationTable) WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error) {
	var workspaceID string
	if err := t.model(ctx).Where("id = ?", entryID).Limit(1).Pluck("workspace_id", &workspaceID).Error; err != nil {
		return "", err
	}
	if workspaceID == "" {
		return "", t.NotFound
	}
	return workspaceID, nil
}

func (t ChannelConversationTable) ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	var ids []string
	if err := t.model(ctx).Where("workspace_id = ?", workspaceID).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (t ChannelConversationTable) RecordInbound(ctx context.Context, id string, at time.Time) error {
	return t.touchClocks(ctx, id, at, "last_customer_message_at")
}

func (t ChannelConversationTable) RecordOutbound(ctx context.Context, id string, at time.Time) error {
	return t.touchClocks(ctx, id, at, "last_agent_message_at")
}

func (t ChannelConversationTable) touchClocks(ctx context.Context, id string, at time.Time, sideColumn string) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return t.affected(t.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"last_message_at": gorm.Expr("GREATEST(COALESCE(last_message_at, ?), ?)", at, at),
		sideColumn:        gorm.Expr("GREATEST(COALESCE("+sideColumn+", ?), ?)", at, at),
	}))
}

func (t ChannelConversationTable) StatusForEntry(ctx context.Context, id string) (string, error) {
	var status string
	if err := t.model(ctx).Where("id = ?", id).Limit(1).Pluck("COALESCE(conversation_status, '')", &status).Error; err != nil {
		return "", err
	}
	return status, nil
}

func (t ChannelConversationTable) SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error {
	return t.affected(t.model(ctx).Where("id = ?", id).Updates(StatusUpdates(write)))
}

func (t ChannelConversationTable) SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error {
	return t.affected(t.model(ctx).Where("id = ?", id).Update("automation_enabled", enabled))
}

func (t ChannelConversationTable) CountByStatus(ctx context.Context, workspaceID, containerID string) (map[string]int64, error) {
	type row struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}
	const statusExpr = "COALESCE(NULLIF(conversation_status, ''), 'new')"

	query := t.model(ctx).
		Select(statusExpr + " AS status, COUNT(*) AS cnt").
		Where("deleted_at IS NULL").
		Where("last_message_at IS NOT NULL")
	switch {
	case containerID != "":
		query = query.Where(t.ContainerColumn+" = ?", containerID)
	case workspaceID != "":
		query = query.Where("workspace_id = ?", workspaceID)
	default:
		return map[string]int64{}, nil
	}

	var rows []row
	if err := query.Group(statusExpr).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, rw := range rows {
		out[rw.Status] = rw.Count
	}
	return out, nil
}
