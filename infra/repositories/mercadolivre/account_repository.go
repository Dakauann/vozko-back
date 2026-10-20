package mercadolivre_repository

import (
	"context"

	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const maxDueBatch = 500

var healthCheckedStatuses = []string{
	string(mldomain.StatusConnected),
	string(mldomain.StatusNeedsReconnect),
	string(mldomain.StatusRevoked),
}

var purgeableStatuses = []string{
	string(mldomain.StatusRevoked),
	string(mldomain.StatusDisconnected),
}

type accountRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAccountRepository(db *gorm.DB) mldomain.AccountRepository {
	return &accountRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *accountRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.MercadoLivreAccount{})
}

func (r *accountRepository) Create(ctx context.Context, a *mldomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	record := toAccountSchema(a)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return mldomain.ErrAccountAlreadyLinked
		}
		return err
	}
	a.ID = record.ID
	a.CreatedAt = record.CreatedAt
	a.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *accountRepository) Update(ctx context.Context, a *mldomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", a.ID).Updates(map[string]any{
		"department_id":          a.DepartmentID,
		"nickname":               a.Nickname,
		"agent_id":               a.AgentID,
		"workflow_id":            a.WorkflowID,
		"pipeline_id":            a.PipelineID,
		"enable_agent_responses": a.EnableAgentResponses,
		"enable_workflow":        a.EnableWorkflow,
		"automation_disclosure":  strings.TrimSpace(a.AutomationDisclosure),
	}), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) Reconnect(ctx context.Context, a *mldomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Unscoped().Model(&schema.MercadoLivreAccount{}).
		Where("id = ? AND workspace_id = ?", a.ID, a.WorkspaceID).
		Updates(map[string]any{
			"deleted_at":           nil,
			"site_id":              a.SiteID,
			"nickname":             a.Nickname,
			"access_token":         piigorm.NewEncrypted(a.AccessToken),
			"refresh_token":        piigorm.NewEncrypted(a.RefreshToken),
			"access_expires_at":    a.AccessExpiresAt,
			"refresh_expires_at":   a.RefreshExpiresAt,
			"granted_scopes":       strings.Join(a.GrantedScopes, " "),
			"can_read_questions":   a.CanReadQuestions,
			"can_answer_questions": a.CanAnswerQuestions,
			"can_read_messages":    a.CanReadMessages,
			"can_send_messages":    a.CanSendMessages,
			"status":               string(a.Status),
			"status_reason":        a.StatusReason,
			"purge_after":          nil,
			"last_reconciled_at":   nil,
		})
	if result.Error != nil && database.IsUniqueViolation(result.Error) {
		return mldomain.ErrAccountAlreadyLinked
	}
	return database.AffectedOne(result, mldomain.ErrAccountNotFound)
}

func (r *accountRepository) UpdateStatus(ctx context.Context, id string, status mldomain.Status, reason string) error {
	if !status.Valid() {
		return mldomain.ErrInvalidStatus
	}
	updates := map[string]any{"status": string(status), "status_reason": reason}
	if status != mldomain.StatusConnected {
		updates["access_token"] = piigorm.Null()
		updates["refresh_token"] = piigorm.Null()
		updates["access_expires_at"] = nil
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(updates), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) UpdateCapabilities(ctx context.Context, id string, caps mldomain.AccountCapabilities) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"can_read_questions":   caps.CanReadQuestions,
		"can_answer_questions": caps.CanAnswerQuestions,
		"can_read_messages":    caps.CanReadMessages,
		"can_send_messages":    caps.CanSendMessages,
	}), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) UpdateNickname(ctx context.Context, id, nickname string) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("nickname", strings.TrimSpace(nickname)), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) FindByID(ctx context.Context, id string) (*mldomain.Account, error) {
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *accountRepository) FindByIDs(ctx context.Context, ids []string) ([]*mldomain.Account, error) {
	records, err := database.FindByIDs[schema.MercadoLivreAccount](ctx, r.db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*mldomain.Account, 0, len(records))
	for i := range records {
		out = append(out, toAccountDomain(&records[i]))
	}
	return out, nil
}

func (r *accountRepository) FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*mldomain.Account, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, mldomain.ErrWorkspaceIDRequired
	}
	return r.first(r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID))
}

func (r *accountRepository) FindLiveByMLUserID(ctx context.Context, mlUserID string) (*mldomain.Account, error) {
	if !mldomain.ValidNumericID(mlUserID) {
		return nil, mldomain.ErrAccountNotFound
	}
	return r.first(r.db.WithContext(ctx).Where("ml_user_id = ?", mlUserID))
}

func (r *accountRepository) FindByWorkspaceAndMLUserIDUnscoped(ctx context.Context, workspaceID, mlUserID string) (*mldomain.Account, error) {
	return r.first(r.db.WithContext(ctx).Unscoped().
		Where("workspace_id = ? AND ml_user_id = ?", workspaceID, mlUserID).
		Order("updated_at DESC"))
}

func (r *accountRepository) first(query *gorm.DB) (*mldomain.Account, error) {
	var record schema.MercadoLivreAccount
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, mldomain.ErrAccountNotFound
	}
	return toAccountDomain(&record), nil
}

func (r *accountRepository) Restore(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Unscoped().Model(&schema.MercadoLivreAccount{}).
		Where("id = ?", id).Updates(map[string]any{"deleted_at": nil, "purge_after": nil})
	if result.Error != nil && database.IsUniqueViolation(result.Error) {
		return mldomain.ErrAccountAlreadyLinked
	}
	return database.AffectedOne(result, mldomain.ErrAccountNotFound)
}

func (r *accountRepository) ListByWorkspace(ctx context.Context, in mldomain.ListAccountsInput) (*shared.PaginatedResult[*mldomain.Account], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, mldomain.ErrWorkspaceIDRequired
	}
	query := r.model(ctx).Where("workspace_id = ?", in.WorkspaceID)
	if s := strings.TrimSpace(in.Search); s != "" {
		query = query.Where("(nickname ILIKE ? OR ml_user_id = ?)", "%"+escapeLike(s)+"%", s)
	}
	if in.Status != nil {
		query = query.Where("status = ?", string(*in.Status))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	pagination := shared.NormalizePagination(in.Options.Pagination)
	var records []schema.MercadoLivreAccount
	if err := query.Order("created_at DESC").Order("id").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(toAccountDomains(records), pagination, total), nil
}

func (r *accountRepository) ListDueForRefresh(ctx context.Context, expiresBefore time.Time, limit int) ([]*mldomain.Account, error) {
	return r.list(r.db.WithContext(ctx).
		Where("status = ?", string(mldomain.StatusConnected)).
		Where("refresh_token IS NOT NULL").
		Where("(access_expires_at IS NULL OR access_expires_at < ?)", expiresBefore).
		Order("access_expires_at ASC NULLS FIRST").Order("id"), limit)
}

func (r *accountRepository) ListDueForReconcile(ctx context.Context, reconciledBefore time.Time, limit int) ([]*mldomain.Account, error) {
	return r.list(r.db.WithContext(ctx).
		Where("status = ?", string(mldomain.StatusConnected)).
		Where("(last_reconciled_at IS NULL OR last_reconciled_at < ?)", reconciledBefore).
		Order("last_reconciled_at ASC NULLS FIRST").Order("id"), limit)
}

func (r *accountRepository) ListDueForHealthCheck(ctx context.Context, checkedBefore time.Time, limit int) ([]*mldomain.Account, error) {
	return r.list(r.db.WithContext(ctx).
		Where("status IN ?", healthCheckedStatuses).
		Where("(last_health_check_at IS NULL OR last_health_check_at < ?)", checkedBefore).
		Order("last_health_check_at ASC NULLS FIRST").Order("id"), limit)
}

func (r *accountRepository) ListDueForPurge(ctx context.Context, now time.Time, limit int) ([]*mldomain.Account, error) {
	return r.list(r.db.WithContext(ctx).Unscoped().
		Where("purge_after IS NOT NULL AND purge_after <= ?", now).
		Where("status IN ?", purgeableStatuses).
		Order("purge_after ASC").Order("id"), limit)
}

func (r *accountRepository) list(query *gorm.DB, limit int) ([]*mldomain.Account, error) {
	if limit <= 0 || limit > maxDueBatch {
		limit = maxDueBatch
	}
	var records []schema.MercadoLivreAccount
	if err := query.Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return toAccountDomains(records), nil
}

func (r *accountRepository) MarkReconciled(ctx context.Context, id string, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("last_reconciled_at", at), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) MarkHealthChecked(ctx context.Context, id string, at time.Time) error {
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Update("last_health_check_at", at), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) Disconnect(ctx context.Context, id string, status mldomain.Status, reason string, purgeAfter time.Time) error {
	if status != mldomain.StatusDisconnected && status != mldomain.StatusRevoked {
		return mldomain.ErrStatusTransition
	}
	return database.AffectedOne(r.model(ctx).Where("id = ?", id).Updates(map[string]any{
		"status":            string(status),
		"status_reason":     reason,
		"access_token":      piigorm.Null(),
		"refresh_token":     piigorm.Null(),
		"access_expires_at": nil,
		"purge_after":       purgeAfter,
		"deleted_at":        r.now(),
	}), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) DeletePurged(ctx context.Context, id string) error {
	return database.AffectedOne(r.db.WithContext(ctx).Unscoped().
		Where("id = ? AND purge_after IS NOT NULL", id).
		Delete(&schema.MercadoLivreAccount{}), mldomain.ErrAccountNotFound)
}

func (r *accountRepository) LockForRefresh(ctx context.Context, id string, refresh mldomain.RefreshFunc) (bool, error) {
	acquired := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record schema.MercadoLivreAccount
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
		return tx.Model(&schema.MercadoLivreAccount{}).Where("id = ?", id).Updates(updates).Error
	})
	return acquired, err
}

func refreshUpdates(outcome mldomain.RefreshOutcome) (map[string]any, error) {
	updates := map[string]any{}
	if outcome.Status != "" {
		if !outcome.Status.Valid() {
			return nil, mldomain.ErrInvalidStatus
		}
		updates["status"] = string(outcome.Status)
		updates["status_reason"] = outcome.StatusReason
	}
	switch {
	case outcome.Tokens != nil:
		t := outcome.Tokens
		if strings.TrimSpace(t.AccessToken) == "" || strings.TrimSpace(t.RefreshToken) == "" {
			return nil, mldomain.ErrCredentialsInvalid
		}
		updates["access_token"] = piigorm.NewEncrypted(t.AccessToken)
		updates["refresh_token"] = piigorm.NewEncrypted(t.RefreshToken)
		updates["access_expires_at"] = t.AccessExpiresAt
		updates["refresh_expires_at"] = t.RefreshExpiresAt
		if len(t.GrantedScopes) > 0 {
			updates["granted_scopes"] = strings.Join(t.GrantedScopes, " ")
		}
	case outcome.Status != "" && outcome.Status != mldomain.StatusConnected:
		updates["access_token"] = piigorm.Null()
		updates["refresh_token"] = piigorm.Null()
		updates["access_expires_at"] = nil
	}
	return updates, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func toAccountSchema(a *mldomain.Account) *schema.MercadoLivreAccount {
	record := &schema.MercadoLivreAccount{
		ID:                   a.ID,
		WorkspaceID:          a.WorkspaceID,
		DepartmentID:         a.DepartmentID,
		MLUserID:             a.MLUserID,
		SiteID:               a.SiteID,
		Nickname:             a.Nickname,
		AccessExpiresAt:      a.AccessExpiresAt,
		RefreshExpiresAt:     a.RefreshExpiresAt,
		GrantedScopes:        strings.Join(a.GrantedScopes, " "),
		CanReadQuestions:     a.CanReadQuestions,
		CanAnswerQuestions:   a.CanAnswerQuestions,
		CanReadMessages:      a.CanReadMessages,
		CanSendMessages:      a.CanSendMessages,
		Status:               string(a.Status),
		StatusReason:         a.StatusReason,
		LastReconciledAt:     a.LastReconciledAt,
		LastHealthCheckAt:    a.LastHealthCheckAt,
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		PipelineID:           a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		AutomationDisclosure: strings.TrimSpace(a.AutomationDisclosure),
		PurgeAfter:           a.PurgeAfter,
	}
	if a.AccessToken != "" {
		record.AccessToken = piigorm.NewEncrypted(a.AccessToken)
	}
	if a.RefreshToken != "" {
		record.RefreshToken = piigorm.NewEncrypted(a.RefreshToken)
	}
	return record
}

func toAccountDomain(record *schema.MercadoLivreAccount) *mldomain.Account {
	return &mldomain.Account{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		DepartmentID:         record.DepartmentID,
		MLUserID:             record.MLUserID,
		SiteID:               record.SiteID,
		Nickname:             record.Nickname,
		AccessToken:          record.AccessToken.Plain,
		RefreshToken:         record.RefreshToken.Plain,
		AccessExpiresAt:      record.AccessExpiresAt,
		RefreshExpiresAt:     record.RefreshExpiresAt,
		GrantedScopes:        mldomain.ParseScopes(record.GrantedScopes),
		CanReadQuestions:     record.CanReadQuestions,
		CanAnswerQuestions:   record.CanAnswerQuestions,
		CanReadMessages:      record.CanReadMessages,
		CanSendMessages:      record.CanSendMessages,
		Status:               mldomain.Status(record.Status),
		StatusReason:         record.StatusReason,
		LastReconciledAt:     record.LastReconciledAt,
		LastHealthCheckAt:    record.LastHealthCheckAt,
		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		AutomationDisclosure: record.AutomationDisclosure,
		PurgeAfter:           record.PurgeAfter,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}

func toAccountDomains(records []schema.MercadoLivreAccount) []*mldomain.Account {
	out := make([]*mldomain.Account, 0, len(records))
	for i := range records {
		out = append(out, toAccountDomain(&records[i]))
	}
	return out
}
