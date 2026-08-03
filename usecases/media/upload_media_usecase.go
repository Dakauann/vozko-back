package media_usecase

import (
	"bytes"
	"fmt"
	img "image"
	"image/jpeg"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"

	"vozko/domain/media"
)

type UploadMediaUseCase struct {
	mediaRepository media.MediaRepository
	fileStorage     media.FileStorage
	// holdMusic standardizes MediaTypeHoldMusic uploads (small mono MP3). Nil
	// disables hold music uploads with a clear error rather than storing a raw
	// file the hold path could not decode.
	holdMusic media.HoldMusicTranscoder
	// holdMusicGate enforces the plan cap on custom hold music tracks (nil fails
	// closed: hold music uploads rejected).
	holdMusicGate media.HoldMusicQuotaGate
}

func NewUploadMediaUseCase(
	mediaRepository media.MediaRepository,
	fileStorage media.FileStorage,
	holdMusic media.HoldMusicTranscoder,
	holdMusicGate media.HoldMusicQuotaGate,
) media.UploadMediaUseCase {
	return &UploadMediaUseCase{
		mediaRepository: mediaRepository,
		fileStorage:     fileStorage,
		holdMusic:       holdMusic,
		holdMusicGate:   holdMusicGate,
	}
}

// UploadMedia stores a media file owned by the WORKSPACE it was uploaded in, the
// same workspace scoping every other resource (agent, label, department) follows, so a
// workflow can validate and use only media belonging to its own workspace. The caller
// resolves workspaceID from the request's workspace context (middleware.GetWorkspaceID).
func (uc *UploadMediaUseCase) UploadMedia(workspaceID string, mediaData []byte, mediaName string, mediaType media.MediaType, description string) (media.Media, error) {
	if !uc.isValidMediaType(mediaType) {
		return media.Media{}, fmt.Errorf("invalid media type: %s", mediaType)
	}

	const maxFileSizeBytes = 25 * 1024 * 1024
	if len(mediaData) > maxFileSizeBytes {
		return media.Media{}, fmt.Errorf("file too large: maximum allowed size is 25 MB")
	}

	if mediaType == media.MediaTypeProductImage {
		if !uc.isImageFile(mediaName) {
			return media.Media{}, fmt.Errorf("invalid media type: non-image files cannot have the MediaTypeProductImage type")
		}
	}

	totalUploads, err := uc.mediaRepository.CountByWorkspaceID(workspaceID)
	if err != nil {
		return media.Media{}, fmt.Errorf("failed to check workspace uploads: %w", err)
	}

	if totalUploads >= 10000 {
		return media.Media{}, fmt.Errorf("upload limit reached: you cannot upload more than 10000 images")
	}

	// Hold music is standardized at the door: the plan quota gate runs first
	// (cheap), then whatever the user uploaded becomes a small mono MP3 bounded in
	// duration, validated against the exact decoder the hold path uses. The stored
	// object is always .mp3.
	if mediaType == media.MediaTypeHoldMusic {
		if uc.holdMusic == nil || uc.holdMusicGate == nil {
			return media.Media{}, media.ErrHoldMusicNotIncluded
		}
		if err := uc.holdMusicGate.CanUploadHoldMusic(workspaceID); err != nil {
			return media.Media{}, err
		}
		converted, err := uc.holdMusic.ToHoldMusicMP3(mediaData)
		if err != nil {
			return media.Media{}, fmt.Errorf("failed to process hold music audio: %w", err)
		}
		mediaData = converted
		mediaName = strings.TrimSuffix(mediaName, filepath.Ext(mediaName)) + ".mp3"
	}

	err = uc.fileStorage.UploadFile(mediaName, mediaData)
	if err != nil {
		return media.Media{}, fmt.Errorf("failed to upload media: %w", err)
	}

	mediaURL := uc.fileStorage.GetFileURL(mediaName)

	var previewMediaData []byte
	var previewMediaName string
	var previewURL string

	if uc.isImageFile(mediaName) {
		previewMediaData, previewMediaName = uc.createPreviewMedia(mediaData, mediaName)

		if previewMediaData != nil && previewMediaName != "" {
			err = uc.fileStorage.UploadFile(previewMediaName, previewMediaData)
			if err != nil {
				return media.Media{}, fmt.Errorf("failed to upload preview media: %w", err)
			}

			previewURL = uc.fileStorage.GetFileURL(previewMediaName)
		}
	}

	newMedia := media.Media{
		WorkspaceID: workspaceID,
		URL:         mediaURL,
		PreviewURL:  previewURL,
		CreatedAt:   time.Now(),
		Type:        mediaType,
		Description: description,
	}

	err = uc.mediaRepository.CreateMedia(&newMedia)
	if err != nil {
		return media.Media{}, fmt.Errorf("failed to save media record to database: %w", err)
	}

	return newMedia, nil
}

func (uc *UploadMediaUseCase) createPreviewMedia(mediaData []byte, mediaName string) ([]byte, string) {
	src, _, err := img.Decode(bytes.NewReader(mediaData))
	if err != nil {

		return nil, ""
	}

	resized := imaging.Resize(src, 0, 200, imaging.Lanczos)

	ext := strings.ToLower(filepath.Ext(mediaName))
	baseName := strings.TrimSuffix(mediaName, ext)
	previewImageName := baseName + "-preview.jpg"

	var buf bytes.Buffer
	err = jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 60})
	if err != nil {
		return nil, ""
	}

	return buf.Bytes(), previewImageName
}

func (uc *UploadMediaUseCase) isImageFile(mediaName string) bool {
	ext := strings.ToLower(filepath.Ext(mediaName))

	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
}

func (uc *UploadMediaUseCase) isValidMediaType(mediaType media.MediaType) bool {
	switch mediaType {
	case media.MediaTypeProductImage, media.MediaTypeProductVideo, media.MediaTypeVslVideo, media.MediaTypeHtml5,
		media.MediaTypeDocumentPdf, media.MediaTypeDocumentDoc, media.MediaTypeDocument,
		media.MediaTypeAudio, media.MediaTypeSticker, media.MediaTypeHoldMusic:
		return true
	default:
		return false
	}
}
