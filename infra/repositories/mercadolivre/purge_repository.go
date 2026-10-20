package mercadolivre_repository

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	mldomain "vozko/domain/mercadolivre"
	"vozko/infra/database/schema"
)

const deleteQuestionBatchSQL = `DELETE FROM mercadolivre_questions WHERE id IN ` +
	`(SELECT id FROM mercadolivre_questions WHERE account_id = ? ORDER BY id LIMIT ?)`

type purgeRepository struct {
	db *gorm.DB
}

func NewPurgeRepository(db *gorm.DB) mldomain.PurgeRepository {
	return &purgeRepository{db: db}
}

func purgeLimit(limit int) int {
	if limit <= 0 || limit > maxIDsPerQuery {
		return maxIDsPerQuery
	}
	return limit
}

func (r *purgeRepository) ids(ctx context.Context, model any, accountID string, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Unscoped().Model(model).Where("account_id = ?", accountID).
		Order("id").Limit(purgeLimit(limit)).Pluck("id", &ids).Error
	return ids, err
}

func (r *purgeRepository) ConversationIDs(ctx context.Context, accountID string, limit int) ([]string, error) {
	return r.ids(ctx, &schema.MercadoLivreConversation{}, accountID, limit)
}

func (r *purgeRepository) PackThumbnailKeys(ctx context.Context, accountID string, conversationIDs []string) ([]string, error) {
	if len(conversationIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		PackSummary datatypes.JSON
	}
	if err := r.db.WithContext(ctx).Unscoped().Model(&schema.MercadoLivreConversation{}).Select("pack_summary").
		Where("account_id = ? AND id IN ? AND pack_summary IS NOT NULL", accountID, conversationIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var keys []string
	for _, row := range rows {
		var summary mldomain.PackSummary
		if err := json.Unmarshal(row.PackSummary, &summary); err != nil {
			return nil, fmt.Errorf("mercadolivre: a stored pack summary is unreadable, its files cannot be found: %w", err)
		}
		keys = append(keys, summary.ThumbnailKeys()...)
	}
	return keys, nil
}

func (r *purgeRepository) DeleteConversations(ctx context.Context, accountID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().Where("account_id = ? AND id IN ?", accountID, ids).Delete(&schema.MercadoLivreConversation{}).Error
}

func (r *purgeRepository) DeleteQuestions(ctx context.Context, accountID string, limit int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(deleteQuestionBatchSQL, accountID, purgeLimit(limit))
	return result.RowsAffected, result.Error
}

func (r *purgeRepository) ListingThumbnails(ctx context.Context, accountID string, limit int) ([]mldomain.ListingThumbnail, error) {
	var rows []struct {
		ItemID       string
		ThumbnailKey string
	}
	if err := r.db.WithContext(ctx).Model(&schema.MercadoLivreListing{}).Select("item_id, thumbnail_key").
		Where("account_id = ?", accountID).Order("id").Limit(purgeLimit(limit)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]mldomain.ListingThumbnail, 0, len(rows))
	for _, row := range rows {
		out = append(out, mldomain.ListingThumbnail{ItemID: row.ItemID, Key: row.ThumbnailKey})
	}
	return out, nil
}

func (r *purgeRepository) DeleteListings(ctx context.Context, accountID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Where("account_id = ? AND item_id IN ?", accountID, itemIDs).Delete(&schema.MercadoLivreListing{}).Error
}

func (r *purgeRepository) DeleteListingSync(ctx context.Context, accountID string) error {
	return r.db.WithContext(ctx).Where("account_id = ?", accountID).Delete(&schema.MercadoLivreListingSync{}).Error
}

func (r *purgeRepository) ContactIDs(ctx context.Context, accountID string, limit int) ([]string, error) {
	return r.ids(ctx, &schema.MercadoLivreContact{}, accountID, limit)
}

func (r *purgeRepository) DeleteContacts(ctx context.Context, accountID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().Where("account_id = ? AND id IN ?", accountID, ids).Delete(&schema.MercadoLivreContact{}).Error
}
