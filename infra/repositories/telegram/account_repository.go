package telegram_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type accountRepository struct {
	db *gorm.DB
}

// NewAccountRepository builds the Telegram account repository.
func NewAccountRepository(db *gorm.DB) tgdomain.AccountRepository {
	return &accountRepository{db: db}
}

func (r *accountRepository) Create(ctx context.Context, a *tgdomain.Account) error {
	record, err := toAccountSchema(a)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if isUniqueViolation(err) {
			return tgdomain.ErrAccountAlreadyLinked
		}
		return err
	}
	a.ID = record.ID
	a.CreatedAt = record.CreatedAt
	a.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *accountRepository) Update(ctx context.Context, a *tgdomain.Account) error {
	record, err := toAccountSchema(a)
	if err != nil {
		return err
	}
	update := map[string]any{
		"workspace_id":            record.WorkspaceID,
		"department_id":           record.DepartmentID,
		"mode":                    record.Mode,
		"bot_user_id":             record.BotUserID,
		"bot_username":            record.BotUsername,
		"bot_name":                record.BotName,
		"can_connect_to_business": record.CanConnectToBusiness,
		"business_connection_id":  record.BusinessConnectionID,
		"business_user_id":        record.BusinessUserID,
		"business_username":       record.BusinessUsername,
		"business_rights":         record.BusinessRights,
		"business_enabled":        record.BusinessEnabled,
		"agent_id":                record.AgentID,
		"workflow_id":             record.WorkflowID,
		"pipeline_id":             record.PipelineID,
		"enable_agent_responses":  record.EnableAgentResponses,
		"enable_workflow":         record.EnableWorkflow,
		"enable_analysis":         record.EnableAnalysis,
		"enable_auto_staging":     record.EnableAutoStaging,
		"enable_auto_memory":      record.EnableAutoMemory,
		"status":                  record.Status,
		"status_reason":           record.StatusReason,
	}
	// Credentials are written only when supplied: a config-only update must not
	// blank a live token, which would silently take the channel offline.
	if a.BotToken != "" {
		update["bot_token"] = record.BotToken
	}
	if a.WebhookSecret != "" {
		update["webhook_secret"] = record.WebhookSecret
	}

	result := r.db.WithContext(ctx).Model(&schema.TelegramAccount{}).
		Where("id = ?", a.ID).
		Updates(update)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return tgdomain.ErrAccountAlreadyLinked
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) UpdateStatus(ctx context.Context, id string, status tgdomain.Status, reason string) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": string(status), "status_reason": reason})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) UpdateWebhookHealth(ctx context.Context, id string, h tgdomain.WebhookHealth) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"webhook_pending_count": h.PendingCount,
			"webhook_last_error":    truncate(h.LastError, 500),
			"webhook_last_error_at": h.LastErrorAt,
			"webhook_checked_at":    h.CheckedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) SetWebhookRegistered(ctx context.Context, id string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"webhook_set_at":        at,
			"webhook_last_error":    "",
			"webhook_last_error_at": nil,
			"webhook_pending_count": 0,
			"webhook_checked_at":    at,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) FindByID(ctx context.Context, id string) (*tgdomain.Account, error) {
	var record schema.TelegramAccount
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

// FindByIDForWebhook resolves the tenant for an inbound request.
//
// It deliberately does not filter on status: a WEBHOOK_FAILING account is
// exactly the one whose next delivery matters most, and refusing it would turn a
// recoverable health blip into permanent message loss once Telegram's 24h
// retention expires.
func (r *accountRepository) FindByIDForWebhook(ctx context.Context, id string) (*tgdomain.Account, error) {
	return r.FindByID(ctx, id)
}

func (r *accountRepository) FindByBotUserID(ctx context.Context, botUserID int64) (*tgdomain.Account, error) {
	var record schema.TelegramAccount
	if err := r.db.WithContext(ctx).First(&record, "bot_user_id = ?", botUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

// FindByBotUserIDUnscoped includes soft-deleted rows so reconnecting a bot that
// was removed restores it rather than colliding with the unique index.
func (r *accountRepository) FindByBotUserIDUnscoped(ctx context.Context, botUserID int64) (*tgdomain.Account, error) {
	var record schema.TelegramAccount
	if err := r.db.WithContext(ctx).Unscoped().First(&record, "bot_user_id = ?", botUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

// FindByBusinessConnectionID is the ONLY way a business-mode webhook finds its
// tenant: the update carries no bot identity, only the connection id.
func (r *accountRepository) FindByBusinessConnectionID(ctx context.Context, connectionID string) (*tgdomain.Account, error) {
	if strings.TrimSpace(connectionID) == "" {
		return nil, tgdomain.ErrAccountNotFound
	}
	var record schema.TelegramAccount
	if err := r.db.WithContext(ctx).
		First(&record, "business_connection_id = ?", connectionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

func (r *accountRepository) Restore(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Unscoped().Model(&schema.TelegramAccount{}).
		Where("id = ?", id).Update("deleted_at", nil).Error
}

func (r *accountRepository) ListByWorkspace(ctx context.Context, input tgdomain.ListAccountsInput) (*shared.PaginatedResult[*tgdomain.Account], error) {
	query := r.db.WithContext(ctx).Model(&schema.TelegramAccount{}).
		Where("workspace_id = ?", input.WorkspaceID)

	if s := strings.TrimSpace(input.Search); s != "" {
		like := "%" + strings.ToLower(s) + "%"
		query = query.Where("LOWER(bot_username) LIKE ? OR LOWER(bot_name) LIKE ? OR LOWER(business_username) LIKE ?", like, like, like)
	}
	if input.Status != nil {
		query = query.Where("status = ?", string(*input.Status))
	}
	if input.Mode != nil {
		query = query.Where("mode = ?", string(*input.Mode))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	pagination := shared.NormalizePagination(input.Options.Pagination)

	var records []schema.TelegramAccount
	if err := query.Order("created_at DESC").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*tgdomain.Account, 0, len(records))
	for i := range records {
		items = append(items, toAccountDomain(&records[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

// ListForHealthCheck returns accounts whose webhook has not been probed
// recently, oldest first so the cron makes progress under a cap.
func (r *accountRepository) ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*tgdomain.Account, error) {
	if limit <= 0 {
		limit = 100
	}
	var records []schema.TelegramAccount
	if err := r.db.WithContext(ctx).
		Where("status IN ?", []string{string(tgdomain.StatusActive), string(tgdomain.StatusWebhookFailing)}).
		Where("webhook_checked_at IS NULL OR webhook_checked_at < ?", before).
		Order("webhook_checked_at ASC NULLS FIRST").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*tgdomain.Account, 0, len(records))
	for i := range records {
		out = append(out, toAccountDomain(&records[i]))
	}
	return out, nil
}

func (r *accountRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.TelegramAccount{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrAccountNotFound
	}
	return nil
}

// ---------------------------------------------------------------- mapping

func toAccountSchema(a *tgdomain.Account) (*schema.TelegramAccount, error) {
	record := &schema.TelegramAccount{
		ID:                   a.ID,
		WorkspaceID:          a.WorkspaceID,
		DepartmentID:         a.DepartmentID,
		Mode:                 string(a.Mode),
		BotUserID:            a.BotUserID,
		BotUsername:          a.BotUsername,
		BotName:              a.BotName,
		CanConnectToBusiness: a.CanConnectToBusiness,
		BusinessConnectionID: a.BusinessConnectionID,
		BusinessUserID:       a.BusinessUserID,
		BusinessUsername:     a.BusinessUsername,
		BusinessEnabled:      a.BusinessEnabled,
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		PipelineID:           a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		EnableAnalysis:       a.EnableAnalysis,
		EnableAutoStaging:    a.EnableAutoStaging,
		EnableAutoMemory:     a.EnableAutoMemory,
		Status:               string(a.Status),
		StatusReason:         a.StatusReason,
	}
	// Credentials are only ever written when present, so a partial update cannot
	// blank a live token by omitting it.
	if a.BotToken != "" {
		record.BotToken = piigorm.NewEncrypted(a.BotToken)
	}
	if a.WebhookSecret != "" {
		record.WebhookSecret = piigorm.NewEncrypted(a.WebhookSecret)
	}
	if a.BusinessRights != nil {
		encoded, err := json.Marshal(a.BusinessRights)
		if err != nil {
			return nil, err
		}
		record.BusinessRights = datatypes.JSON(encoded)
	}
	return record, nil
}

func toAccountDomain(record *schema.TelegramAccount) *tgdomain.Account {
	a := &tgdomain.Account{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		DepartmentID:         record.DepartmentID,
		Mode:                 tgdomain.Mode(record.Mode),
		BotUserID:            record.BotUserID,
		BotUsername:          record.BotUsername,
		BotName:              record.BotName,
		CanConnectToBusiness: record.CanConnectToBusiness,
		BotToken:             record.BotToken.Plain,
		WebhookSecret:        record.WebhookSecret.Plain,
		WebhookSetAt:         record.WebhookSetAt,
		WebhookPendingCount:  record.WebhookPendingCount,
		WebhookLastError:     record.WebhookLastError,
		WebhookLastErrorAt:   record.WebhookLastErrorAt,
		WebhookCheckedAt:     record.WebhookCheckedAt,
		BusinessConnectionID: record.BusinessConnectionID,
		BusinessUserID:       record.BusinessUserID,
		BusinessUsername:     record.BusinessUsername,
		BusinessEnabled:      record.BusinessEnabled,
		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		EnableAutoMemory:     record.EnableAutoMemory,
		Status:               tgdomain.Status(record.Status),
		StatusReason:         record.StatusReason,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
	if len(record.BusinessRights) > 0 {
		var rights tgdomain.BusinessRights
		if err := json.Unmarshal(record.BusinessRights, &rights); err == nil {
			a.BusinessRights = &rights
		}
	}
	return a
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isUniqueViolation recognises a Postgres unique-index conflict without
// depending on the driver's concrete error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
