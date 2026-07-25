package workflow_repository

import (
	"errors"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type webhookRepository struct {
	db *gorm.DB
}

func NewWorkflowWebhookRepository(db *gorm.DB) workflow.WorkflowWebhookRepository {
	return &webhookRepository{db: db}
}

func (r *webhookRepository) Create(webhook *workflow.WorkflowWebhook) error {
	dbW := toWebhookSchema(webhook)
	if err := r.db.Create(dbW).Error; err != nil {
		return err
	}
	webhook.ID = dbW.ID
	webhook.CreatedAt = dbW.CreatedAt
	webhook.UpdatedAt = dbW.UpdatedAt
	return nil
}

func (r *webhookRepository) Update(webhook *workflow.WorkflowWebhook) error {
	updates := map[string]interface{}{
		"token":       webhook.Token,
		"auth_mode":   string(webhook.AuthMode),
		"secret":      webhook.Secret,
		"header_name": webhook.HeaderName,
		"method":      webhook.Method,
		"active":      webhook.Active,
	}
	return r.db.Model(&schema.WorkflowWebhookSchema{}).Where("id = ?", webhook.ID).Updates(updates).Error
}

func (r *webhookRepository) FindByToken(token string) (*workflow.WorkflowWebhook, error) {
	var dbW schema.WorkflowWebhookSchema
	if err := r.db.Where("token = ?", token).First(&dbW).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toWebhookDomain(&dbW), nil
}

func (r *webhookRepository) FindByWorkflowID(workflowID string) (*workflow.WorkflowWebhook, error) {
	var dbW schema.WorkflowWebhookSchema
	if err := r.db.Where("workflow_id = ?", workflowID).First(&dbW).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toWebhookDomain(&dbW), nil
}

func (r *webhookRepository) Delete(workflowID string) error {
	return r.db.Where("workflow_id = ?", workflowID).Delete(&schema.WorkflowWebhookSchema{}).Error
}

func toWebhookSchema(wh *workflow.WorkflowWebhook) *schema.WorkflowWebhookSchema {
	return &schema.WorkflowWebhookSchema{
		ID:          wh.ID,
		WorkflowID:  wh.WorkflowID,
		WorkspaceID: wh.WorkspaceID,
		Token:       wh.Token,
		AuthMode:    string(wh.AuthMode),
		Secret:      wh.Secret,
		HeaderName:  wh.HeaderName,
		Method:      wh.Method,
		Active:      wh.Active,
	}
}

func toWebhookDomain(dbW *schema.WorkflowWebhookSchema) *workflow.WorkflowWebhook {
	return &workflow.WorkflowWebhook{
		ID:          dbW.ID,
		WorkflowID:  dbW.WorkflowID,
		WorkspaceID: dbW.WorkspaceID,
		Token:       dbW.Token,
		AuthMode:    workflow.WebhookAuthMode(dbW.AuthMode),
		Secret:      dbW.Secret,
		HeaderName:  dbW.HeaderName,
		Method:      dbW.Method,
		Active:      dbW.Active,
		CreatedAt:   dbW.CreatedAt,
		UpdatedAt:   dbW.UpdatedAt,
	}
}
