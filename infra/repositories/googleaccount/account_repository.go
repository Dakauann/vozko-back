package googleaccount_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/googleaccount"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) googleaccount.Repository {
	return &repository{db: db}
}

func (r *repository) SaveConnected(ctx context.Context, a *googleaccount.Account) error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return googleaccount.ErrWorkspaceRequired
	}
	if strings.TrimSpace(a.RefreshToken) == "" {
		return googleaccount.ErrNoRefreshToken
	}
	email := googleaccount.NormalizeEmail(a.Email)
	if email == "" {
		return googleaccount.ErrEmailMissing
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing schema.GoogleAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND email = ?", a.WorkspaceID, email).Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		expires := a.AccessExpiresAt
		if existing.ID == "" {
			record := &schema.GoogleAccount{
				WorkspaceID:     a.WorkspaceID,
				Email:           email,
				AccessToken:     piigorm.NewEncrypted(a.AccessToken),
				RefreshToken:    piigorm.NewEncrypted(a.RefreshToken),
				AccessExpiresAt: &expires,
				Scopes:          strings.Join(a.Scopes, " "),
				Status:          string(googleaccount.StatusConnected),
				ConnectedBy:     optionalID(a.ConnectedBy),
			}
			if err := tx.Create(record).Error; err != nil {
				return err
			}
			*a = *toDomain(record)
			return nil
		}
		updates := map[string]any{
			"access_token":      piigorm.NewEncrypted(a.AccessToken),
			"refresh_token":     piigorm.NewEncrypted(a.RefreshToken),
			"access_expires_at": expires,
			"scopes":            strings.Join(a.Scopes, " "),
			"status":            string(googleaccount.StatusConnected),
			"status_reason":     "",
			"connected_by":      optionalID(a.ConnectedBy),
			"updated_at":        time.Now().UTC(),
		}
		if err := tx.Model(&schema.GoogleAccount{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return err
		}
		var fresh schema.GoogleAccount
		if err := tx.Where("id = ?", existing.ID).Take(&fresh).Error; err != nil {
			return err
		}
		*a = *toDomain(&fresh)
		return nil
	})
}

func (r *repository) FindByID(ctx context.Context, id string) (*googleaccount.Account, error) {
	var record schema.GoogleAccount
	if err := r.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, googleaccount.ErrAccountNotFound
	}
	return toDomain(&record), nil
}

func (r *repository) FindByEmail(ctx context.Context, workspaceID, email string) (*googleaccount.Account, error) {
	var record schema.GoogleAccount
	if err := r.db.WithContext(ctx).Where("workspace_id = ? AND email = ?", workspaceID, googleaccount.NormalizeEmail(email)).
		Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, googleaccount.ErrAccountNotFound
	}
	return toDomain(&record), nil
}

func (r *repository) FindBound(ctx context.Context, workspaceID string, product googleaccount.Product) (*googleaccount.Account, error) {
	var record schema.GoogleAccount
	err := r.db.WithContext(ctx).
		Joins("JOIN google_account_bindings b ON b.account_id = google_accounts.id").
		Where("b.workspace_id = ? AND b.product = ?", workspaceID, string(product)).
		Limit(1).Find(&record).Error
	if err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, googleaccount.ErrNotConnected
	}
	return toDomain(&record), nil
}

func (r *repository) ListBound(ctx context.Context, workspaceID string) ([]googleaccount.BoundAccount, error) {
	var bindings []schema.GoogleAccountBinding
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("product").Find(&bindings).Error; err != nil {
		return nil, err
	}
	out := make([]googleaccount.BoundAccount, 0, len(bindings))
	for _, b := range bindings {
		account, err := r.FindByID(ctx, b.AccountID)
		if err != nil {
			return nil, err
		}
		out = append(out, googleaccount.BoundAccount{Product: googleaccount.Product(b.Product), Account: account})
	}
	return out, nil
}

func (r *repository) Bind(ctx context.Context, b googleaccount.Binding) error {
	if !b.Product.Valid() {
		return googleaccount.ErrUnknownProduct
	}
	record := schema.GoogleAccountBinding{WorkspaceID: b.WorkspaceID, Product: string(b.Product), AccountID: b.AccountID}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "product"}},
		DoUpdates: clause.Assignments(map[string]any{"account_id": b.AccountID, "updated_at": time.Now().UTC()}),
	}).Create(&record).Error
}

func (r *repository) Unbind(ctx context.Context, workspaceID string, product googleaccount.Product) (string, error) {
	var removed []schema.GoogleAccountBinding
	err := r.db.WithContext(ctx).Clauses(clause.Returning{}).
		Where("workspace_id = ? AND product = ?", workspaceID, string(product)).
		Delete(&removed).Error
	if err != nil {
		return "", err
	}
	if len(removed) == 0 {
		return "", googleaccount.ErrNotConnected
	}
	return removed[0].AccountID, nil
}

func (r *repository) CountBindings(ctx context.Context, accountID string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&schema.GoogleAccountBinding{}).Where("account_id = ?", accountID).Count(&count).Error
	return int(count), err
}

func (r *repository) Disconnect(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&schema.GoogleAccount{}).Where("id = ?", id).Updates(map[string]any{
		"status":            string(googleaccount.StatusDisconnected),
		"status_reason":     "",
		"access_token":      piigorm.Null(),
		"refresh_token":     piigorm.Null(),
		"access_expires_at": nil,
		"updated_at":        time.Now().UTC(),
	}).Error
}

func (r *repository) ConnectedElsewhere(ctx context.Context, email, exceptID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&schema.GoogleAccount{}).
		Where("email = ? AND id <> ? AND status <> ?", googleaccount.NormalizeEmail(email), exceptID, string(googleaccount.StatusDisconnected)).
		Count(&count).Error
	return count > 0, err
}

func (r *repository) ListDueForRefresh(ctx context.Context, expiresBefore time.Time, limit int) ([]*googleaccount.Account, error) {
	var records []schema.GoogleAccount
	err := r.db.WithContext(ctx).
		Where("status = ?", string(googleaccount.StatusConnected)).
		Where("(access_expires_at IS NULL OR access_expires_at < ?)", expiresBefore).
		Order("access_expires_at ASC NULLS FIRST").Order("id").
		Limit(limit).Find(&records).Error
	if err != nil {
		return nil, err
	}
	out := make([]*googleaccount.Account, 0, len(records))
	for i := range records {
		out = append(out, toDomain(&records[i]))
	}
	return out, nil
}

func (r *repository) LockForRefresh(ctx context.Context, id string, refresh googleaccount.RefreshFunc) (bool, error) {
	acquired := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record schema.GoogleAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ?", id).Limit(1).Find(&record).Error; err != nil {
			return err
		}
		if record.ID == "" {
			return nil
		}
		acquired = true
		outcome, err := refresh(ctx, toDomain(&record))
		if err != nil {
			return err
		}
		updates, err := refreshUpdates(outcome)
		if err != nil || len(updates) == 0 {
			return err
		}
		return tx.Model(&schema.GoogleAccount{}).Where("id = ?", id).Updates(updates).Error
	})
	return acquired, err
}

func refreshUpdates(outcome googleaccount.RefreshOutcome) (map[string]any, error) {
	updates := map[string]any{}
	if outcome.Status != "" {
		if !outcome.Status.Valid() {
			return nil, errors.New("googleaccount: invalid status in refresh outcome")
		}
		updates["status"] = string(outcome.Status)
		updates["status_reason"] = truncate(outcome.StatusReason, 255)
	}
	switch {
	case outcome.Tokens != nil:
		t := outcome.Tokens
		if strings.TrimSpace(t.AccessToken) == "" || strings.TrimSpace(t.RefreshToken) == "" {
			return nil, googleaccount.ErrIncompleteGrant
		}
		updates["access_token"] = piigorm.NewEncrypted(t.AccessToken)
		updates["refresh_token"] = piigorm.NewEncrypted(t.RefreshToken)
		updates["access_expires_at"] = t.AccessExpiresAt
		if len(t.Scopes) > 0 {
			updates["scopes"] = strings.Join(t.Scopes, " ")
		}
	case outcome.Status != "" && outcome.Status != googleaccount.StatusConnected:
		updates["access_token"] = piigorm.Null()
		updates["refresh_token"] = piigorm.Null()
		updates["access_expires_at"] = nil
	}
	if len(updates) > 0 {
		updates["updated_at"] = time.Now().UTC()
	}
	return updates, nil
}

func toDomain(record *schema.GoogleAccount) *googleaccount.Account {
	a := &googleaccount.Account{
		ID:           record.ID,
		WorkspaceID:  record.WorkspaceID,
		Email:        record.Email,
		AccessToken:  record.AccessToken.Plain,
		RefreshToken: record.RefreshToken.Plain,
		Scopes:       strings.Fields(record.Scopes),
		Status:       googleaccount.Status(record.Status),
		StatusReason: record.StatusReason,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
	}
	if record.AccessExpiresAt != nil {
		a.AccessExpiresAt = *record.AccessExpiresAt
	}
	if record.ConnectedBy != nil {
		a.ConnectedBy = *record.ConnectedBy
	}
	return a
}

func optionalID(id string) *string {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return &id
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
