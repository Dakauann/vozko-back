package conversation_repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type adOriginRepository struct {
	db *gorm.DB
}

func NewAdOriginRepository(db *gorm.DB) conversation.AdOriginRepository {
	return &adOriginRepository{db: db}
}

func (r *adOriginRepository) Claim(origin *conversation.AdOrigin) (bool, error) {
	row := schema.ConversationAdOrigin{
		EntryID:   origin.EntryID,
		EntryType: string(origin.EntryType),
		AdID:      origin.AdID,
		Platform:  string(origin.Platform),
		Title:     origin.Title,
		SourceURL:  origin.SourceURL,
		ClickID:    origin.ClickID,
		SourceType: origin.SourceType,
		ArrivedAt:  origin.ArrivedAt,
	}
	if origin.ImageMediaID != "" {
		row.ImageMediaID = &origin.ImageMediaID
	}
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	return result.RowsAffected == 1, result.Error
}

func (r *adOriginRepository) Get(entryID string, entryType shared.EntryType) (*conversation.AdOrigin, error) {
	var row schema.ConversationAdOrigin
	err := r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	origin := &conversation.AdOrigin{
		EntryID:   row.EntryID,
		EntryType: shared.EntryType(row.EntryType),
		AdID:      row.AdID,
		Platform:  conversation.AdPlatform(row.Platform),
		Title:     row.Title,
		SourceURL:  row.SourceURL,
		ClickID:    row.ClickID,
		SourceType: row.SourceType,
		ArrivedAt:  row.ArrivedAt,
	}
	if row.ImageMediaID != nil {
		origin.ImageMediaID = *row.ImageMediaID
	}
	return origin, nil
}
