package mercadolivre_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const claimListingSyncSQL = `INSERT INTO mercadolivre_listing_syncs (account_id, started_at) VALUES (?, ?) ` +
	`ON CONFLICT (account_id) DO UPDATE SET started_at = EXCLUDED.started_at ` +
	`WHERE mercadolivre_listing_syncs.started_at < ? ` +
	`RETURNING account_id`

const listingUpsertBatch = 100

type listingRepository struct {
	db *gorm.DB
}

func NewListingRepository(db *gorm.DB) mldomain.ListingRepository {
	return &listingRepository{db: db}
}

func (r *listingRepository) model(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.MercadoLivreListing{})
}

func (r *listingRepository) UpsertMany(ctx context.Context, listings []*mldomain.Listing) error {
	records := make([]*schema.MercadoLivreListing, 0, len(listings))
	for _, l := range listings {
		if l == nil {
			continue
		}
		if !mldomain.ValidItemID(l.ItemID) || strings.TrimSpace(l.AccountID) == "" || strings.TrimSpace(l.WorkspaceID) == "" {
			return mldomain.ErrInvalidResource
		}
		records = append(records, &schema.MercadoLivreListing{
			WorkspaceID: l.WorkspaceID, AccountID: l.AccountID, ItemID: l.ItemID, Title: l.Title, Permalink: l.Permalink,
			ThumbnailSource: l.ThumbnailSource, Status: l.Status, SyncedAt: l.SyncedAt,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"workspace_id", "title", "permalink", "status", "synced_at", "updated_at"}),
	}).CreateInBatches(records, listingUpsertBatch).Error
}

func (r *listingRepository) SetThumbnail(ctx context.Context, accountID, itemID, key, source string) error {
	return database.AffectedOne(r.model(ctx).Where("account_id = ? AND item_id = ?", accountID, itemID).Updates(map[string]any{
		"thumbnail_key":    key,
		"thumbnail_source": source,
	}), mldomain.ErrListingNotFound)
}

func (r *listingRepository) FindByItemID(ctx context.Context, accountID, itemID string) (*mldomain.Listing, error) {
	var record schema.MercadoLivreListing
	if err := r.db.WithContext(ctx).Where("account_id = ? AND item_id = ?", accountID, itemID).Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, mldomain.ErrListingNotFound
	}
	return toListingDomain(&record), nil
}

func (r *listingRepository) FindByItemIDs(ctx context.Context, accountID string, itemIDs []string) ([]*mldomain.Listing, error) {
	unique := shared.UniqueNonEmpty(itemIDs)
	out := make([]*mldomain.Listing, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(unique))
		var records []schema.MercadoLivreListing
		if err := r.db.WithContext(ctx).Where("account_id = ? AND item_id IN ?", accountID, unique[start:end]).Find(&records).Error; err != nil {
			return nil, err
		}
		for i := range records {
			out = append(out, toListingDomain(&records[i]))
		}
	}
	return out, nil
}

func (r *listingRepository) ListByAccount(ctx context.Context, in mldomain.ListListingsInput) (*mldomain.ListingList, error) {
	limit := mldomain.NormalizeListLimit(in.Limit)
	query := r.model(ctx).Where("account_id = ?", in.AccountID)
	if s := strings.TrimSpace(in.Status); s != "" {
		query = query.Where("status = ?", s)
	}
	if s := strings.TrimSpace(in.Search); s != "" {
		query = query.Where("(title ILIKE ? OR item_id = ?)", "%"+escapeLike(s)+"%", strings.ToUpper(s))
	}
	if in.After != nil {
		query = query.Where("(created_at, id) < (?, ?)", in.After.At, in.After.ID)
	}
	var records []schema.MercadoLivreListing
	if err := query.Order("created_at DESC").Order("id DESC").Limit(limit + 1).Find(&records).Error; err != nil {
		return nil, err
	}
	out := &mldomain.ListingList{Items: make([]*mldomain.Listing, 0, min(len(records), limit))}
	for i := range records {
		if i == limit {
			last := out.Items[limit-1]
			out.Next = &shared.Keyset{At: last.CreatedAt, ID: last.ID}
			break
		}
		out.Items = append(out.Items, toListingDomain(&records[i]))
	}
	return out, nil
}

func (r *listingRepository) ClaimSync(ctx context.Context, accountID string, now, syncedBefore time.Time) (bool, error) {
	var claimed []string
	if err := r.db.WithContext(ctx).Raw(claimListingSyncSQL, accountID, now, syncedBefore).Scan(&claimed).Error; err != nil {
		return false, err
	}
	return len(claimed) == 1, nil
}

func (r *listingRepository) ReleaseSync(ctx context.Context, accountID string) error {
	return r.db.WithContext(ctx).Where("account_id = ?", accountID).Delete(&schema.MercadoLivreListingSync{}).Error
}

func toListingDomain(r *schema.MercadoLivreListing) *mldomain.Listing {
	return &mldomain.Listing{
		ID:              r.ID,
		WorkspaceID:     r.WorkspaceID,
		AccountID:       r.AccountID,
		ItemID:          r.ItemID,
		Title:           r.Title,
		Permalink:       r.Permalink,
		ThumbnailKey:    r.ThumbnailKey,
		ThumbnailSource: r.ThumbnailSource,
		Status:          r.Status,
		SyncedAt:        r.SyncedAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}
