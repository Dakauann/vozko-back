package media_repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/media"
	"vozko/infra/database/schema"
)

type MediaRepository struct {
	db *gorm.DB
}

func NewMediaRepository(db *gorm.DB) *MediaRepository {
	return &MediaRepository{db: db}
}

func (r *MediaRepository) GetMediaByID(mediaID string) (*media.Media, error) {
	if mediaID == "" {
		return nil, nil
	}

	var mediaSchema schema.Media
	if err := r.db.First(&mediaSchema, "id = ?", mediaID).Error; err != nil {
		return nil, err
	}

	mediaItem := &media.Media{
		ID:          mediaSchema.ID,
		WorkspaceID: mediaSchema.WorkspaceID,
		URL:         mediaSchema.URL,
		PreviewURL:  mediaSchema.PreviewURL,
		CreatedAt:   mediaSchema.CreatedAt,
		Type:        media.MediaType(mediaSchema.Type),
		Description: mediaSchema.Description,
	}

	return mediaItem, nil
}

func (r *MediaRepository) CountByWorkspaceIDAndType(workspaceID string, mediaType media.MediaType) (int64, error) {
	var count int64
	err := r.db.Model(&schema.Media{}).
		Where("workspace_id = ? AND type = ?", workspaceID, string(mediaType)).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *MediaRepository) CreateMedia(media *media.Media) error {
	if media.ID == "" {
		media.ID = uuid.New().String()
	}

	mediaSchema := &schema.Media{
		ID:          media.ID,
		WorkspaceID: media.WorkspaceID,
		URL:         media.URL,
		PreviewURL:  media.PreviewURL,
		CreatedAt:   media.CreatedAt,
		Type:        schema.MediaType(media.Type),
		Description: media.Description,
	}

	if err := r.db.Create(mediaSchema).Error; err != nil {
		return err
	}

	media.ID = mediaSchema.ID
	return nil
}

func (r *MediaRepository) ListMediasByWorkspace(workspaceID string) ([]media.Media, error) {
	var mediaSchemas []schema.Media
	if err := r.db.Table("medias").Where("workspace_id = ?", workspaceID).Find(&mediaSchemas).Error; err != nil {
		return nil, err
	}

	var medias []media.Media
	for _, mediaSchema := range mediaSchemas {
		medias = append(medias, media.Media{
			ID:          mediaSchema.ID,
			WorkspaceID: mediaSchema.WorkspaceID,
			URL:         mediaSchema.URL,
			PreviewURL:  mediaSchema.PreviewURL,
			CreatedAt:   mediaSchema.CreatedAt,
			Type:        media.MediaType(mediaSchema.Type),
			Description: mediaSchema.Description,
		})
	}

	return medias, nil
}

func (r *MediaRepository) DeleteMedia(mediaID string) error {
	if err := r.db.Delete(&schema.Media{}, mediaID).Error; err != nil {
		return err
	}
	return nil
}

func (r *MediaRepository) CountWorkspaceUploadsToday(workspaceID string) (int64, error) {
	var count int64
	if err := r.db.Model(&schema.Media{}).Where("workspace_id = ? AND created_at >= ?", workspaceID, time.Now().Truncate(24*time.Hour)).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *MediaRepository) GetMediasByIDs(mediaIDs []string) ([]media.Media, error) {
	if len(mediaIDs) == 0 {
		return []media.Media{}, nil
	}

	var mediaSchemas []schema.Media
	if err := r.db.Where("id IN ?", mediaIDs).Find(&mediaSchemas).Error; err != nil {
		return nil, err
	}

	medias := make([]media.Media, 0, len(mediaSchemas))
	for _, mediaSchema := range mediaSchemas {
		medias = append(medias, media.Media{
			ID:          mediaSchema.ID,
			WorkspaceID: mediaSchema.WorkspaceID,
			URL:         mediaSchema.URL,
			PreviewURL:  mediaSchema.PreviewURL,
			CreatedAt:   mediaSchema.CreatedAt,
			Type:        media.MediaType(mediaSchema.Type),
			Description: mediaSchema.Description,
		})
	}

	return medias, nil
}

func (r *MediaRepository) MediaExists(mediaID string) (bool, error) {
	if mediaID == "" {
		return false, nil
	}

	var count int64
	if err := r.db.Model(&schema.Media{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *MediaRepository) CountByWorkspaceID(workspaceID string) (int64, error) {
	var count int64
	if err := r.db.Model(&schema.Media{}).Where("workspace_id = ?", workspaceID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

var _ media.MediaRepository = (*MediaRepository)(nil)
