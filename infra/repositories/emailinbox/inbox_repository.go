package emailinbox_repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailaddress"
	"vozko/domain/emailinbox"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type inboxRepository struct {
	db *gorm.DB
}

func NewInboxRepository(db *gorm.DB) emailinbox.InboxRepository {
	return &inboxRepository{db: db}
}

func (r *inboxRepository) Ensure(ctx context.Context, inbox emailinbox.Inbox) (*emailinbox.Inbox, error) {
	id, workspaceID := strings.TrimSpace(inbox.ID), strings.TrimSpace(inbox.WorkspaceID)
	if id == "" || workspaceID == "" {
		return nil, emailinbox.ErrInboxNotFound
	}
	record := &schema.EmailInbox{ID: id, WorkspaceID: workspaceID, Name: strings.TrimSpace(inbox.Name), Address: string(inbox.Address)}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			Where: clause.Where{Exprs: []clause.Expression{clause.Expr{
				SQL: "email_inboxes.workspace_id = excluded.workspace_id AND (email_inboxes.name IS DISTINCT FROM excluded.name OR email_inboxes.address IS DISTINCT FROM excluded.address)",
			}}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":       gorm.Expr("excluded.name"),
				"address":    gorm.Expr("excluded.address"),
				"updated_at": gorm.Expr("NOW()"),
			}),
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByID(ctx, workspaceID, id)
}

func (r *inboxRepository) FindByID(ctx context.Context, workspaceID, id string) (*emailinbox.Inbox, error) {
	return r.first(ctx, "id = ? AND workspace_id = ?", id, workspaceID)
}

func (r *inboxRepository) FindByIDUnscoped(ctx context.Context, id string) (*emailinbox.Inbox, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *inboxRepository) first(ctx context.Context, query string, args ...any) (*emailinbox.Inbox, error) {
	var record schema.EmailInbox
	if err := r.db.WithContext(ctx).Where(query, args...).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, emailinbox.ErrInboxNotFound
		}
		return nil, err
	}
	return toInboxDomain(&record), nil
}

func (r *inboxRepository) FindByIDsUnscoped(ctx context.Context, ids []string) ([]*emailinbox.Inbox, error) {
	records, err := database.FindByIDs[schema.EmailInbox](ctx, r.db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*emailinbox.Inbox, 0, len(records))
	for i := range records {
		out = append(out, toInboxDomain(&records[i]))
	}
	return out, nil
}

func (r *inboxRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*emailinbox.Inbox, error) {
	var records []schema.EmailInbox
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID).
		Order("created_at ASC, id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*emailinbox.Inbox, 0, len(records))
	for i := range records {
		out = append(out, toInboxDomain(&records[i]))
	}
	return out, nil
}

func (r *inboxRepository) Update(ctx context.Context, inbox *emailinbox.Inbox) error {
	result := r.db.WithContext(ctx).Model(&schema.EmailInbox{}).
		Where("id = ? AND workspace_id = ?", inbox.ID, inbox.WorkspaceID).
		Updates(map[string]any{
			"department_id":          inbox.DepartmentID,
			"agent_id":               inbox.AgentID,
			"workflow_id":            inbox.WorkflowID,
			"pipeline_id":            inbox.PipelineID,
			"enable_agent_responses": inbox.EnableAgentResponses,
			"enable_workflow":        inbox.EnableWorkflow,
			"enable_analysis":        inbox.EnableAnalysis,
			"enable_auto_staging":    inbox.EnableAutoStaging,
			"enable_auto_memory":     inbox.EnableAutoMemory,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return emailinbox.ErrInboxNotFound
	}
	return nil
}

func toInboxDomain(record *schema.EmailInbox) *emailinbox.Inbox {
	return &emailinbox.Inbox{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		Name:                 record.Name,
		Address:              emailaddress.Address(record.Address),
		DepartmentID:         record.DepartmentID,
		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		EnableAutoMemory:     record.EnableAutoMemory,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}
