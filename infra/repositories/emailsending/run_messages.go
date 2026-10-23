package emailsending_repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
)

type runMessages struct {
	db *gorm.DB
}

func NewRunMessages(db *gorm.DB) es.RunMessages {
	return &runMessages{db: db}
}

func (r *runMessages) ListByRun(ctx context.Context, workspaceID string, kind es.RunKind, runID string) ([]es.Message, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(string(kind)) == "" || strings.TrimSpace(runID) == "" {
		return []es.Message{}, nil
	}
	var rows []schema.EmailMessage
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND run_kind = ? AND run_id = ?", workspaceID, string(kind), runID).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]es.Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, toMessage(row))
	}
	return out, nil
}
