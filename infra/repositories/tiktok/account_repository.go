package tiktok_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

const maxDueBatch = 500

var healthCheckedStatuses = []string{string(ttdomain.StatusConnected), string(ttdomain.StatusNeedsReconnect)}

var purgeableStatuses = []string{string(ttdomain.StatusRevoked), string(ttdomain.StatusDisconnected)}

type accountRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAccountRepository(db *gorm.DB) ttdomain.AccountRepository {
	return &accountRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *accountRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.TikTokAccount{})
}

func (r *accountRepository) Create(ctx context.Context, a *ttdomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	record := toAccountSchema(a)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return ttdomain.ErrAccountAlreadyLinked
		}
		return err
	}
	a.ID = record.ID
	a.CreatedAt = record.CreatedAt
	a.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *accountRepository) Update(ctx context.Context, a *ttdomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", a.ID).Updates(map[string]any{
		"department_id":          a.DepartmentID,
		"agent_id":               a.AgentID,
		"workflow_id":            a.WorkflowID,
		"pipeline_id":            a.PipelineID,
		"enable_agent_responses": a.EnableAgentResponses,
		"enable_workflow":        a.EnableWorkflow,
		"enable_analysis":        a.EnableAnalysis,
		"enable_auto_staging":    a.EnableAutoStaging,
		"enable_auto_memory":     a.EnableAutoMemory,
		"automation_disclosure":  strings.TrimSpace(a.AutomationDisclosure),
	}), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) Reconnect(ctx context.Context, a *ttdomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	updates := map[string]any{
		"deleted_at":         nil,
		"access_token":       piigorm.NewEncrypted(a.AccessToken),
		"refresh_token":      piigorm.NewEncrypted(a.RefreshToken),
		"access_expires_at":  a.AccessExpiresAt,
		"refresh_expires_at": a.RefreshExpiresAt,
		"status":             string(a.Status),
		"status_reason":      a.StatusReason,
		"purge_after":        nil,
	}
	for k, v := range profileColumns(ttdomain.ProfileUpdate{
		Username: a.Username, DisplayName: a.DisplayName, AvatarKey: a.AvatarKey,
		HasAdvancedAccess: a.HasAdvancedAccess, IsBusinessAccount: a.IsBusinessAccount,
		GrantedScopes: a.GrantedScopes, Capabilities: a.Capabilities,
	}) {
		updates[k] = v
	}
	result := r.db.WithContext(ctx).Unscoped().Model(&schema.TikTokAccount{}).
		Where("id = ? AND workspace_id = ?", a.ID, a.WorkspaceID).Updates(updates)
	if result.Error != nil && database.IsUniqueViolation(result.Error) {
		return ttdomain.ErrAccountAlreadyLinked
	}
	return database.AffectedOne(result, ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) UpdateStatus(ctx context.Context, id string, status ttdomain.Status, reason string) error {
	if !status.Valid() {
		return ttdomain.ErrInvalidStatus
	}
	updates := map[string]any{"status": string(status), "status_reason": reason}
	if status != ttdomain.StatusConnected {
		updates["access_token"] = piigorm.Null()
		updates["refresh_token"] = piigorm.Null()
		updates["access_expires_at"] = nil
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(updates), ttdomain.ErrAccountNotFound)
}

func profileColumns(p ttdomain.ProfileUpdate) map[string]any {
	return map[string]any{
		"username":                 strings.TrimSpace(p.Username),
		"display_name":             strings.TrimSpace(p.DisplayName),
		"avatar_key":               strings.TrimSpace(p.AvatarKey),
		"has_advanced_access":      p.HasAdvancedAccess,
		"is_business_account":      p.IsBusinessAccount,
		"granted_scopes":           strings.Join(p.GrantedScopes, ","),
		"can_read_videos":          p.Capabilities.CanReadVideos,
		"can_read_comments":        p.Capabilities.CanReadComments,
		"can_manage_comments":      p.Capabilities.CanManageComments,
		"can_receive_messages":     p.Capabilities.CanReceiveMessages,
		"can_send_messages":        p.Capabilities.CanSendMessages,
		"can_manage_inbox_profile": p.Capabilities.CanManageInboxProfile,
	}
}

func (r *accountRepository) UpdateProfile(ctx context.Context, id string, p ttdomain.ProfileUpdate) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(profileColumns(p)), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) LearnUniqueIdentifier(ctx context.Context, id, uniqueIdentifier string) error {
	if !ttdomain.ValidOpaqueID(uniqueIdentifier) {
		return ttdomain.ErrOpenIDInvalid
	}
	return r.model(ctx).Where("id = ? AND unique_identifier = ''", id).Update("unique_identifier", uniqueIdentifier).Error
}

func (r *accountRepository) SetWebhooksHealthy(ctx context.Context, id string, healthy bool) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("webhooks_healthy", healthy), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) FindByID(ctx context.Context, id string) (*ttdomain.Account, error) {
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *accountRepository) FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*ttdomain.Account, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ttdomain.ErrWorkspaceIDRequired
	}
	return r.first(r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID))
}

func (r *accountRepository) FindLiveByOpenID(ctx context.Context, openID string) (*ttdomain.Account, error) {
	if !ttdomain.ValidOpaqueID(openID) {
		return nil, ttdomain.ErrAccountNotFound
	}
	return r.first(r.db.WithContext(ctx).Where("open_id = ?", openID))
}

func (r *accountRepository) FindByWorkspaceAndOpenIDUnscoped(ctx context.Context, workspaceID, openID string) (*ttdomain.Account, error) {
	return r.first(r.db.WithContext(ctx).Unscoped().
		Where("workspace_id = ? AND open_id = ?", workspaceID, openID).
		Order("updated_at DESC"))
}

func (r *accountRepository) first(query *gorm.DB) (*ttdomain.Account, error) {
	var record schema.TikTokAccount
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, ttdomain.ErrAccountNotFound
	}
	return toAccountDomain(&record), nil
}

func (r *accountRepository) Restore(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Unscoped().Model(&schema.TikTokAccount{}).
		Where("id = ?", id).Updates(map[string]any{"deleted_at": nil, "purge_after": nil})
	if result.Error != nil && database.IsUniqueViolation(result.Error) {
		return ttdomain.ErrAccountAlreadyLinked
	}
	return database.AffectedOne(result, ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) ListByWorkspace(ctx context.Context, in ttdomain.ListAccountsInput) (*shared.PaginatedResult[*ttdomain.Account], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, ttdomain.ErrWorkspaceIDRequired
	}
	query := r.model(ctx).Where("workspace_id = ?", in.WorkspaceID)
	if s := strings.TrimSpace(in.Search); s != "" {
		like := sqlexpr.LikeContains(s)
		query = query.Where("(display_name ILIKE ? OR username ILIKE ?)", like, like)
	}
	if in.Status != nil {
		query = query.Where("status = ?", string(*in.Status))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	pagination := shared.NormalizePagination(in.Options.Pagination)
	var records []schema.TikTokAccount
	if err := query.Order("created_at DESC").Order("id").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(toAccountDomains(records), pagination, total), nil
}

func (r *accountRepository) ListConnected(ctx context.Context, afterID string, limit int) ([]*ttdomain.Account, error) {
	query := r.db.WithContext(ctx).Where("status = ?", string(ttdomain.StatusConnected))
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}
	return r.list(query.Order("id"), limit)
}

func (r *accountRepository) ListDueForRefresh(ctx context.Context, expiresBefore time.Time, limit int) ([]*ttdomain.Account, error) {
	return r.list(r.db.WithContext(ctx).
		Where("status = ?", string(ttdomain.StatusConnected)).
		Where("refresh_token IS NOT NULL").
		Where("(access_expires_at IS NULL OR access_expires_at < ?)", expiresBefore).
		Order("access_expires_at ASC NULLS FIRST").Order("id"), limit)
}

func (r *accountRepository) ListDueForHealthCheck(ctx context.Context, checkedBefore time.Time, limit int) ([]*ttdomain.Account, error) {
	return r.list(r.db.WithContext(ctx).
		Where("status IN ?", healthCheckedStatuses).
		Where("(last_health_check_at IS NULL OR last_health_check_at < ?)", checkedBefore).
		Order("last_health_check_at ASC NULLS FIRST").Order("id"), limit)
}

func (r *accountRepository) ListDueForPurge(ctx context.Context, now time.Time, limit int) ([]*ttdomain.Account, error) {
	return r.list(r.db.WithContext(ctx).Unscoped().
		Where("purge_after IS NOT NULL AND purge_after <= ?", now).
		Where("status IN ?", purgeableStatuses).
		Order("purge_after ASC").Order("id"), limit)
}

func (r *accountRepository) list(query *gorm.DB, limit int) ([]*ttdomain.Account, error) {
	if limit <= 0 || limit > maxDueBatch {
		limit = maxDueBatch
	}
	var records []schema.TikTokAccount
	if err := query.Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return toAccountDomains(records), nil
}

func (r *accountRepository) MarkHealthChecked(ctx context.Context, id string, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("last_health_check_at", at), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) Disconnect(ctx context.Context, id string, status ttdomain.Status, reason string, purgeAfter time.Time) error {
	if status != ttdomain.StatusDisconnected && status != ttdomain.StatusRevoked {
		return ttdomain.ErrStatusTransition
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"status":            string(status),
		"status_reason":     reason,
		"access_token":      piigorm.Null(),
		"refresh_token":     piigorm.Null(),
		"access_expires_at": nil,
		"webhooks_healthy":  false,
		"purge_after":       purgeAfter,
		"deleted_at":        r.now(),
	}), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) Revoke(ctx context.Context, id string, reason string, purgeAfter time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"status":            string(ttdomain.StatusRevoked),
		"status_reason":     reason,
		"access_token":      piigorm.Null(),
		"refresh_token":     piigorm.Null(),
		"access_expires_at": nil,
		"webhooks_healthy":  false,
		"purge_after":       purgeAfter,
	}), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) DeletePurged(ctx context.Context, id string) error {
	return database.AffectedOne(r.db.WithContext(ctx).Unscoped().
		Where("id = ? AND purge_after IS NOT NULL", id).
		Delete(&schema.TikTokAccount{}), ttdomain.ErrAccountNotFound)
}

func (r *accountRepository) LockForRefresh(ctx context.Context, id string, refresh ttdomain.RefreshFunc) (bool, error) {
	acquired := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record schema.TikTokAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ?", id).Limit(1).Find(&record).Error; err != nil {
			return err
		}
		if record.ID == "" {
			return nil
		}
		acquired = true
		outcome, err := refresh(ctx, toAccountDomain(&record))
		if err != nil {
			return err
		}
		updates, err := refreshUpdates(outcome)
		if err != nil || len(updates) == 0 {
			return err
		}
		return tx.Model(&schema.TikTokAccount{}).Where("id = ?", id).Updates(updates).Error
	})
	return acquired, err
}

func refreshUpdates(outcome ttdomain.RefreshOutcome) (map[string]any, error) {
	updates := map[string]any{}
	if outcome.Status != "" {
		if !outcome.Status.Valid() {
			return nil, ttdomain.ErrInvalidStatus
		}
		updates["status"] = string(outcome.Status)
		updates["status_reason"] = outcome.StatusReason
	}
	if outcome.PurgeAfter != nil {
		updates["purge_after"] = *outcome.PurgeAfter
	}
	switch {
	case outcome.Tokens != nil:
		t := outcome.Tokens
		if strings.TrimSpace(t.AccessToken) == "" || strings.TrimSpace(t.RefreshToken) == "" {
			return nil, ttdomain.ErrGrantIncomplete
		}
		updates["access_token"] = piigorm.NewEncrypted(t.AccessToken)
		updates["refresh_token"] = piigorm.NewEncrypted(t.RefreshToken)
		updates["access_expires_at"] = t.AccessExpiresAt
		updates["refresh_expires_at"] = t.RefreshExpiresAt
		if len(t.GrantedScopes) > 0 {
			updates["granted_scopes"] = strings.Join(t.GrantedScopes, ",")
		}
	case outcome.Status != "" && outcome.Status != ttdomain.StatusConnected:
		updates["access_token"] = piigorm.Null()
		updates["refresh_token"] = piigorm.Null()
		updates["access_expires_at"] = nil
	}
	return updates, nil
}
