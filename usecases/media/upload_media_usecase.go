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
	"vozko/domain/studio"
)

type UploadMediaUseCase struct {
	mediaRepository media.MediaRepository
	fileStorage     media.FileStorage
}

func NewUploadMediaUseCase(
	mediaRepository media.MediaRepository,
	fileStorage media.FileStorage,
) media.UploadMediaUseCase {
	return &UploadMediaUseCase{
		mediaRepository: mediaRepository,
		fileStorage:     fileStorage,
	}
}

func (uc *UploadMediaUseCase) UploadMedia(workspaceID string, mediaData []byte, mediaName string, mediaType media.MediaType, description string) (media.Media, error) {
	const maxFileSizeBytes = 25 * 1024 * 1024
	if len(mediaData) > maxFileSizeBytes {
		return media.Media{}, fmt.Errorf("file too large: maximum allowed size is 25 MB")
	}

	if err := uploadIssue(mediaName, mediaData, mediaType); err != nil {
		return media.Media{}, err
	}

	if mediaType == media.MediaTypeProductImage {
		if !uc.isImageFile(mediaName) {
			return media.Media{}, fmt.Errorf("invalid media type: non-image files cannot have the MediaTypeProductImage type")
		}
	}

	if err := withinUploadLimit(uc.mediaRepository, workspaceID); err != nil {
		return media.Media{}, err
	}

	err := uc.fileStorage.UploadFile(mediaName, mediaData, "")
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
			err = uc.fileStorage.UploadFile(previewMediaName, previewMediaData, "")
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

func uploadIssue(mediaName string, mediaData []byte, mediaType media.MediaType) error {
	switch mediaType {
	case media.MediaTypeLut:
		return lutIssue(mediaName, mediaData)
	case media.MediaTypeLottie:
		if err := studio.ParseLottie(mediaName, mediaData); err != nil {
			return fmt.Errorf("invalid Lottie file: %w", err)
		}
		return nil
	}
	if !validUploadType(mediaType) {
		return fmt.Errorf("invalid media type: %s", mediaType)
	}
	return nil
}

func lutIssue(mediaName string, mediaData []byte) error {
	if strings.ToLower(filepath.Ext(mediaName)) != studio.CubeFileExt {
		return fmt.Errorf("invalid media type: a LUT must be a %s file", studio.CubeFileExt)
	}
	if _, err := studio.ParseCube(mediaData); err != nil {
		return fmt.Errorf("invalid LUT file: %w", err)
	}
	return nil
}

func (uc *UploadMediaUseCase) isImageFile(mediaName string) bool {
	ext := strings.ToLower(filepath.Ext(mediaName))

	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
}
