package media_usecase

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/media"
)

const maxWorkspaceUploads = 10_000

var ErrUploadLimit = errors.New("upload limit reached: you cannot upload more than 10000 images")

type fileURLs interface {
	GetFileURL(key string) string
}

type RegisterStoredMediaUseCase struct {
	mediaRepository media.MediaRepository
	files           fileURLs
}

func NewRegisterStoredMediaUseCase(mediaRepository media.MediaRepository, files fileURLs) *RegisterStoredMediaUseCase {
	return &RegisterStoredMediaUseCase{mediaRepository: mediaRepository, files: files}
}

func (uc *RegisterStoredMediaUseCase) RegisterStored(workspaceID, id, key string, mediaType media.MediaType, description string) (media.Media, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(key) == "" {
		return media.Media{}, fmt.Errorf("a stored media needs an id and a key")
	}
	if !validUploadType(mediaType) {
		return media.Media{}, fmt.Errorf("invalid media type: %s", mediaType)
	}
	if err := withinUploadLimit(uc.mediaRepository, workspaceID); err != nil {
		return media.Media{}, err
	}
	created := media.Media{ID: id, WorkspaceID: workspaceID, URL: uc.files.GetFileURL(key), CreatedAt: time.Now(), Type: mediaType, Description: description}
	if err := uc.mediaRepository.CreateMedia(&created); err != nil {
		return media.Media{}, fmt.Errorf("failed to save media record to database: %w", err)
	}
	return created, nil
}

func withinUploadLimit(repository media.MediaRepository, workspaceID string) error {
	total, err := repository.CountByWorkspaceID(workspaceID)
	if err != nil {
		return fmt.Errorf("failed to check workspace uploads: %w", err)
	}
	if total >= maxWorkspaceUploads {
		return ErrUploadLimit
	}
	return nil
}

func validUploadType(mediaType media.MediaType) bool {
	switch mediaType {
	case media.MediaTypeProductImage, media.MediaTypeProductVideo, media.MediaTypeVslVideo, media.MediaTypeHtml5,
		media.MediaTypeDocumentPdf, media.MediaTypeDocumentDoc, media.MediaTypeDocument,
		media.MediaTypeAudio, media.MediaTypeSticker:
		return true
	default:
		return false
	}
}
