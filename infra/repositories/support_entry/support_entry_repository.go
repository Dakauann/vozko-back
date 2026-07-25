package support_entry_repository

import (
	"gorm.io/gorm"

	"vozko/domain/shared"
	si "vozko/domain/support_inbox"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) si.EntryRepository {
	return &repository{db: db}
}

func (r *repository) Create(entry *si.SupportEntry) error {
	record := schema.SupportEntry{
		ID:           entry.ID,
		InboxID:      entry.InboxID,
		ContactName:  entry.ContactName,
		ContactEmail: entry.ContactEmail,
		SourceURL:    entry.SourceURL,
	}
	return r.db.Create(&record).Error
}

func (r *repository) FindByID(id string) (*si.SupportEntry, error) {
	var record schema.SupportEntry
	if err := r.db.Where("id = ?", id).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, si.ErrEntryNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) Delete(id string) error {
	result := r.db.Delete(&schema.SupportEntry{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return si.ErrEntryNotFound
	}
	return nil
}

func (r *repository) List(input si.ListEntriesInput) (*shared.PaginatedResult[*si.SupportEntry], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.SupportEntry{}).Where("inbox_id = ?", input.InboxID)
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	var records []schema.SupportEntry
	if err := r.db.Where("inbox_id = ?", input.InboxID).
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*si.SupportEntry, 0, len(records))
	for i := range records {
		items = append(items, mapToDomain(&records[i]))
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) CountByInboxID(inboxID string) (int64, error) {
	var count int64
	if err := r.db.Model(&schema.SupportEntry{}).Where("inbox_id = ?", inboxID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func mapToDomain(record *schema.SupportEntry) *si.SupportEntry {
	return &si.SupportEntry{
		ID:           record.ID,
		InboxID:      record.InboxID,
		ContactName:  record.ContactName,
		ContactEmail: record.ContactEmail,
		SourceURL:    record.SourceURL,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
	}
}
