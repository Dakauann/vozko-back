package conversation_repository

import (
	"errors"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type mediaRepository struct {
	db *gorm.DB
}

func NewMediaRepository(db *gorm.DB) conversation.ConversationMediaRepository {
	return &mediaRepository{db: db}
}

func (r *mediaRepository) Create(media *conversation.ConversationMedia) error {
	if media == nil {
		return conversation.ErrMediaRequired
	}

	dbMedia := mapMediaDomainToSchema(media)
	return r.db.Create(dbMedia).Error
}

func (r *mediaRepository) GetByID(id string) (*conversation.ConversationMedia, error) {
	var dbMedia schema.ConversationMedia
	if err := r.db.Where("id = ?", id).First(&dbMedia).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMediaNotFound
		}
		return nil, err
	}
	return mapMediaSchemaToDomain(&dbMedia), nil
}

func (r *mediaRepository) GetInEntry(entryID string, entryType shared.EntryType, id string) (*conversation.ConversationMedia, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, conversation.ErrMediaNotFound
	}
	if _, err := uuid.Parse(entryID); err != nil {
		return nil, conversation.ErrMediaNotFound
	}
	var dbMedia schema.ConversationMedia
	if err := r.db.Where("id = ? AND entry_id = ? AND entry_type = ?", id, entryID, string(entryType)).First(&dbMedia).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMediaNotFound
		}
		return nil, err
	}
	return mapMediaSchemaToDomain(&dbMedia), nil
}

func NewMediaURLReader(db *gorm.DB) conversation.EntryMediaURLReader {
	return &mediaRepository{db: db}
}

func (r *mediaRepository) GetInEntryByURL(entryID string, entryType shared.EntryType, url string) (*conversation.ConversationMedia, error) {
	if _, err := uuid.Parse(entryID); err != nil || url == "" {
		return nil, conversation.ErrMediaNotFound
	}
	var dbMedia schema.ConversationMedia
	if err := r.db.Where("entry_id = ? AND entry_type = ? AND url = ?", entryID, string(entryType), url).Order("created_at DESC").First(&dbMedia).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMediaNotFound
		}
		return nil, err
	}
	return mapMediaSchemaToDomain(&dbMedia), nil
}

func (r *mediaRepository) ListByIDs(ids []string) ([]*conversation.ConversationMedia, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []schema.ConversationMedia
	if err := r.db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*conversation.ConversationMedia, 0, len(rows))
	for i := range rows {
		out = append(out, mapMediaSchemaToDomain(&rows[i]))
	}
	return out, nil
}

func (r *mediaRepository) GetByWhatsAppMediaID(whatsappMediaID string) (*conversation.ConversationMedia, error) {
	var dbMedia schema.ConversationMedia
	if err := r.db.Where("whatsapp_media_id = ?", whatsappMediaID).First(&dbMedia).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMediaNotFound
		}
		return nil, err
	}
	return mapMediaSchemaToDomain(&dbMedia), nil
}

func (r *mediaRepository) ListByEntry(entryID string, entryType shared.EntryType) ([]*conversation.ConversationMedia, error) {
	var dbMedias []schema.ConversationMedia
	if err := r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Order("created_at DESC").
		Find(&dbMedias).Error; err != nil {
		return nil, err
	}

	medias := make([]*conversation.ConversationMedia, 0, len(dbMedias))
	for i := range dbMedias {
		medias = append(medias, mapMediaSchemaToDomain(&dbMedias[i]))
	}
	return medias, nil
}

func (r *mediaRepository) Delete(id string) error {
	result := r.db.Delete(&schema.ConversationMedia{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return conversation.ErrMediaNotFound
	}
	return nil
}

func (r *mediaRepository) DeleteByEntry(entryID string, entryType shared.EntryType) error {
	return r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Delete(&schema.ConversationMedia{}).Error
}

func mapMediaDomainToSchema(media *conversation.ConversationMedia) *schema.ConversationMedia {
	if media == nil {
		return nil
	}
	return &schema.ConversationMedia{
		ID:               media.ID,
		EntryID:          media.EntryID,
		EntryType:        string(media.EntryType),
		Type:             string(media.Type),
		MimeType:         media.MimeType,
		URL:              media.URL,
		OriginalFilename: media.OriginalFilename,
		SizeBytes:        media.SizeBytes,
		DurationSeconds:  media.DurationSeconds,
		WhatsAppMediaID:  media.WhatsAppMediaID,
		Width:            media.Layout.Width,
		Height:           media.Layout.Height,
		Thumbhash:        media.Layout.Thumbhash,
		Purpose:          string(media.Purpose),
		CreatedAt:        media.CreatedAt,
	}
}

func mapMediaSchemaToDomain(media *schema.ConversationMedia) *conversation.ConversationMedia {
	if media == nil {
		return nil
	}
	return &conversation.ConversationMedia{
		ID:               media.ID,
		EntryID:          media.EntryID,
		EntryType:        shared.EntryType(media.EntryType),
		Type:             conversation.MediaType(media.Type),
		MimeType:         media.MimeType,
		URL:              media.URL,
		OriginalFilename: media.OriginalFilename,
		SizeBytes:        media.SizeBytes,
		DurationSeconds:  media.DurationSeconds,
		WhatsAppMediaID:  media.WhatsAppMediaID,
		Layout:           conversation.MediaLayout{Width: media.Width, Height: media.Height, Thumbhash: media.Thumbhash},
		Purpose:          conversation.MediaPurpose(media.Purpose),
		CreatedAt:        media.CreatedAt,
	}
}
