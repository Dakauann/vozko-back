package tiktok_repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/database/schema"
)

const storedObjectKeysSQL = `SELECT key FROM (
	SELECT avatar_key AS key FROM tiktok_contacts WHERE account_id = ? AND avatar_key <> ''
	UNION ALL
	SELECT thumbnail_key AS key FROM tiktok_videos WHERE account_id = ? AND thumbnail_key <> ''
	UNION ALL
	SELECT avatar_key AS key FROM tiktok_accounts WHERE id = ? AND avatar_key <> ''
) keys LIMIT ?`

var accountOwnedTables = []string{"tiktok_comments", "tiktok_videos"}

type purgeRepository struct {
	db *gorm.DB
}

func NewPurgeRepository(db *gorm.DB) ttdomain.PurgeRepository {
	return &purgeRepository{db: db}
}

func (r *purgeRepository) ConversationIDsForPurge(ctx context.Context, accountID string, limit int) ([]string, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, ttdomain.ErrAccountNotFound
	}
	var ids []string
	err := r.db.WithContext(ctx).Unscoped().Model(&schema.TikTokConversation{}).
		Where("account_id = ?", accountID).Order("id").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (r *purgeRepository) DeleteConversations(ctx context.Context, accountID string, ids []string) error {
	unique := shared.UniqueNonEmpty(ids)
	if len(unique) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().
		Where("account_id = ? AND id IN ?", accountID, unique).
		Delete(&schema.TikTokConversation{}).Error
}

func (r *purgeRepository) StoredObjectKeys(ctx context.Context, accountID string, limit int) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).Raw(storedObjectKeysSQL, accountID, accountID, accountID, limit).Scan(&keys).Error
	return shared.UniqueNonEmpty(keys), err
}

func (r *purgeRepository) ForgetObjectKeys(ctx context.Context, accountID string, keys []string) error {
	unique := shared.UniqueNonEmpty(keys)
	if len(unique) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Model(&schema.TikTokContact{}).Where("account_id = ? AND avatar_key IN ?", accountID, unique).Update("avatar_key", "").Error; err != nil {
			return err
		}
		if err := tx.Model(&schema.TikTokVideo{}).Where("account_id = ? AND thumbnail_key IN ?", accountID, unique).Update("thumbnail_key", "").Error; err != nil {
			return err
		}
		return tx.Unscoped().Model(&schema.TikTokAccount{}).Where("id = ? AND avatar_key IN ?", accountID, unique).Update("avatar_key", "").Error
	})
}

func (r *purgeRepository) DeleteAccountRows(ctx context.Context, accountID string, limit int) (bool, error) {
	if strings.TrimSpace(accountID) == "" {
		return false, ttdomain.ErrAccountNotFound
	}
	for _, table := range accountOwnedTables {
		result := r.db.WithContext(ctx).Exec(
			"DELETE FROM "+table+" WHERE id IN (SELECT id FROM "+table+" WHERE account_id = ? LIMIT ?)",
			accountID, limit,
		)
		if result.Error != nil {
			return false, result.Error
		}
		if int(result.RowsAffected) == limit {
			return false, nil
		}
	}
	return true, nil
}

func (r *purgeRepository) ContactIDsForPurge(ctx context.Context, accountID string, limit int) ([]string, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, ttdomain.ErrAccountNotFound
	}
	var ids []string
	err := r.db.WithContext(ctx).Unscoped().Model(&schema.TikTokContact{}).
		Where("account_id = ?", accountID).Order("id").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (r *purgeRepository) DeleteContacts(ctx context.Context, accountID string, ids []string) error {
	unique := shared.UniqueNonEmpty(ids)
	if len(unique) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().
		Where("account_id = ? AND id IN ?", accountID, unique).
		Delete(&schema.TikTokContact{}).Error
}
