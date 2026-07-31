package instagram_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type accountRepository struct {
	db *gorm.DB
}

// NewAccountRepository builds the Instagram account repository.
func NewAccountRepository(db *gorm.DB) igdomain.AccountRepository {
	return &accountRepository{db: db}
}

func (r *accountRepository) Create(ctx context.Context, a *igdomain.Account) error {
	record := toAccountSchema(a)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if isUniqueViolation(err) {
			return igdomain.ErrAccountAlreadyLinked
		}
		return err
	}
	a.ID = record.ID
	return nil
}

func (r *accountRepository) Update(ctx context.Context, a *igdomain.Account) error {
	update := map[string]any{
		"workspace_id":           a.WorkspaceID,
		"department_id":          a.DepartmentID,
		"username":               a.Username,
		"name":                   a.Name,
		"profile_picture_url":    a.ProfilePictureURL,
		"account_type":           a.AccountType,
		"followers_count":        a.FollowersCount,
		"follows_count":          a.FollowsCount,
		"media_count":            a.MediaCount,
		"granted_scopes":         strings.Join(a.GrantedScopes, ","),
		"agent_id":               a.AgentID,
		"workflow_id":            a.WorkflowID,
		"pipeline_id":            a.PipelineID,
		"enable_agent_responses": a.EnableAgentResponses,
		"enable_workflow":        a.EnableWorkflow,
		"enable_analysis":        a.EnableAnalysis,
		"enable_auto_staging":    a.EnableAutoStaging,
		"status":                 string(a.Status),
		"status_reason":          a.StatusReason,
	}
	// Only rotate the credential when one was supplied, so a config-only update
	// cannot blank a working token.
	if a.AccessToken != "" {
		update["access_token"] = piigorm.NewEncrypted(a.AccessToken)
	}

	result := r.db.WithContext(ctx).Model(&schema.InstagramAccount{}).
		Where("id = ?", a.ID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) UpdateToken(ctx context.Context, id, token string, expiresAt, refreshedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"access_token":       piigorm.NewEncrypted(token),
			"token_expires_at":   expiresAt,
			"token_refreshed_at": refreshedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) UpdateStatus(ctx context.Context, id string, status igdomain.Status, reason string) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": string(status), "status_reason": reason})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) UpdateMessagingHealth(ctx context.Context, id string, healthy bool, checkedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{"messaging_healthy": healthy, "messaging_checked_at": checkedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) SetWebhookSubscribedAt(ctx context.Context, id string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.InstagramAccount{}).
		Where("id = ?", id).
		Update("webhook_subscribed_at", at)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) FindByID(ctx context.Context, id string) (*igdomain.Account, error) {
	var record schema.InstagramAccount
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

func (r *accountRepository) FindByIGUserID(ctx context.Context, igUserID string) (*igdomain.Account, error) {
	var record schema.InstagramAccount
	if err := r.db.WithContext(ctx).First(&record, "ig_user_id = ?", igUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

// FindByIGUserIDUnscoped includes soft-deleted rows so re-onboarding a
// previously disconnected account restores it rather than colliding with the
// global unique index on ig_user_id.
func (r *accountRepository) FindByIGUserIDUnscoped(ctx context.Context, igUserID string) (*igdomain.Account, error) {
	var record schema.InstagramAccount
	if err := r.db.WithContext(ctx).Unscoped().First(&record, "ig_user_id = ?", igUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, igdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return toAccountDomain(&record), nil
}

func (r *accountRepository) Restore(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Unscoped().Model(&schema.InstagramAccount{}).
		Where("id = ?", id).Update("deleted_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) ListByWorkspace(ctx context.Context, input igdomain.ListAccountsInput) (*shared.PaginatedResult[*igdomain.Account], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	query := r.db.WithContext(ctx).Model(&schema.InstagramAccount{}).
		Where("workspace_id = ?", input.WorkspaceID)

	if search := strings.TrimSpace(input.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(username) LIKE ? OR LOWER(name) LIKE ?", like, like)
	}
	if input.Status != nil {
		query = query.Where("status = ?", string(*input.Status))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var records []schema.InstagramAccount
	if err := query.
		Order("created_at DESC").
		Limit(pagination.PageSize).
		Offset(pagination.Offset()).
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*igdomain.Account, 0, len(records))
	for i := range records {
		items = append(items, toAccountDomain(&records[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

// ListDueForTokenRefresh returns connected accounts whose token expires before
// the cutoff and which are old enough to refresh. Instagram rejects a refresh on
// a token younger than 24 hours, so that floor is part of the query rather than
// a caller-side filter.
func (r *accountRepository) ListDueForTokenRefresh(ctx context.Context, before time.Time, limit int) ([]*igdomain.Account, error) {
	if limit <= 0 {
		limit = 50
	}
	floor := time.Now().UTC().Add(-24 * time.Hour)

	var records []schema.InstagramAccount
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(igdomain.StatusConnected)).
		Where("token_expires_at IS NOT NULL AND token_expires_at < ?", before).
		Where("token_refreshed_at IS NULL OR token_refreshed_at < ?", floor).
		Order("token_expires_at ASC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}

	out := make([]*igdomain.Account, 0, len(records))
	for i := range records {
		out = append(out, toAccountDomain(&records[i]))
	}
	return out, nil
}

func (r *accountRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.InstagramAccount{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

// ---------------------------------------------------------------- mapping

func toAccountSchema(a *igdomain.Account) *schema.InstagramAccount {
	record := &schema.InstagramAccount{
		ID:                   a.ID,
		WorkspaceID:          a.WorkspaceID,
		DepartmentID:         a.DepartmentID,
		IGUserID:             a.IGUserID,
		Username:             a.Username,
		Name:                 a.Name,
		ProfilePictureURL:    a.ProfilePictureURL,
		AccountType:          a.AccountType,
		FollowersCount:       a.FollowersCount,
		FollowsCount:         a.FollowsCount,
		MediaCount:           a.MediaCount,
		TokenExpiresAt:       a.TokenExpiresAt,
		TokenRefreshedAt:     a.TokenRefreshedAt,
		GrantedScopes:        strings.Join(a.GrantedScopes, ","),
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		PipelineID:           a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		EnableAnalysis:       a.EnableAnalysis,
		EnableAutoStaging:    a.EnableAutoStaging,
		Status:               string(a.Status),
		StatusReason:         a.StatusReason,
		WebhookSubscribedAt:  a.WebhookSubscribedAt,
		MessagingHealthy:     a.MessagingHealthy,
		MessagingCheckedAt:   a.MessagingCheckedAt,
	}
	if a.AccessToken != "" {
		record.AccessToken = piigorm.NewEncrypted(a.AccessToken)
	}
	return record
}

func toAccountDomain(record *schema.InstagramAccount) *igdomain.Account {
	a := &igdomain.Account{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		DepartmentID:         record.DepartmentID,
		IGUserID:             record.IGUserID,
		Username:             record.Username,
		Name:                 record.Name,
		ProfilePictureURL:    record.ProfilePictureURL,
		AccountType:          record.AccountType,
		FollowersCount:       record.FollowersCount,
		FollowsCount:         record.FollowsCount,
		MediaCount:           record.MediaCount,
		AccessToken:          record.AccessToken.Plain,
		TokenExpiresAt:       record.TokenExpiresAt,
		TokenRefreshedAt:     record.TokenRefreshedAt,
		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		Status:               igdomain.Status(record.Status),
		StatusReason:         record.StatusReason,
		WebhookSubscribedAt:  record.WebhookSubscribedAt,
		MessagingHealthy:     record.MessagingHealthy,
		MessagingCheckedAt:   record.MessagingCheckedAt,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
	if record.GrantedScopes != "" {
		for _, s := range strings.Split(record.GrantedScopes, ",") {
			if s = strings.TrimSpace(s); s != "" {
				a.GrantedScopes = append(a.GrantedScopes, s)
			}
		}
	}
	return a
}

// isUniqueViolation detects a Postgres unique-constraint error without importing
// a driver-specific package, matching how the rest of the repositories sniff it.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "23505")
}
