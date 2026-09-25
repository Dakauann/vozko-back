package template_usecase

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"vozko/domain/media"
	"vozko/domain/whatsapp/template"
)

type setTemplateHeaderMediaUseCase struct {
	templateRepo  template.Repository
	clientFactory template.WhatsAppClientFactory
	files         media.FileReader
}

func NewSetTemplateHeaderMediaUseCase(templateRepo template.Repository, clientFactory template.WhatsAppClientFactory, files media.FileReader) template.SetTemplateHeaderMediaUseCase {
	return &setTemplateHeaderMediaUseCase{
		templateRepo:  templateRepo,
		clientFactory: clientFactory,
		files:         files,
	}
}

func (uc *setTemplateHeaderMediaUseCase) Execute(input template.SetTemplateHeaderMediaInput) error {
	if strings.TrimSpace(input.TemplateID) == "" {
		return template.ErrTemplateNotFound
	}

	tmpl, err := uc.templateRepo.FindByID(input.TemplateID)
	if err != nil {
		return err
	}

	if !tmpl.HasMediaHeader() {
		return template.ErrHeaderMediaURLNotApplicable
	}

	isClearing := input.HeaderMediaURL == nil || *input.HeaderMediaURL == ""
	if isClearing {
		log.Printf("[template-header-media] WARNING: Clearing header media for template %s (%s). Sending this template will fail without header media.", tmpl.Name, tmpl.ID)
		return uc.templateRepo.UpdateHeaderMedia(input.TemplateID, nil, nil)
	}

	if uc.files == nil {
		return template.ErrHeaderMediaOutsideStorage
	}
	key, ours := uc.files.KeyFromURL(*input.HeaderMediaURL)
	if !ours {
		return template.ErrHeaderMediaOutsideStorage
	}

	if tmpl.WABAId == "" {
		return fmt.Errorf("template has no WABA ID, cannot upload media")
	}

	client, err := uc.clientFactory.ClientForWABA(tmpl.WABAId)
	if err != nil {
		return fmt.Errorf("failed to get WhatsApp client for phone: %w", err)
	}

	mediaData, mimeType, err := uc.readMedia(key, *input.HeaderMediaURL)
	if err != nil {
		return fmt.Errorf("failed to read header media: %w", err)
	}
	log.Printf("[template-header-media] Downloaded %d bytes, mime type: %s", len(mediaData), mimeType)

	fileName := path.Base(*input.HeaderMediaURL)
	if fileName == "" || fileName == "." || fileName == "/" {
		ext := ".bin"
		switch {
		case strings.HasPrefix(mimeType, "image/jpeg"):
			ext = ".jpg"
		case strings.HasPrefix(mimeType, "image/png"):
			ext = ".png"
		case strings.HasPrefix(mimeType, "image/webp"):
			ext = ".webp"
		case strings.HasPrefix(mimeType, "video/mp4"):
			ext = ".mp4"
		case strings.HasPrefix(mimeType, "video/"):
			ext = ".mp4"
		case strings.HasPrefix(mimeType, "application/pdf"):
			ext = ".pdf"
		}
		fileName = "header_media" + ext
	}

	log.Printf("[template-header-media] Uploading media to WhatsApp: %s (%s, %d bytes)", fileName, mimeType, len(mediaData))
	uploadCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mediaID, err := client.UploadMedia(uploadCtx, mediaData, fileName, mimeType)
	if err != nil {
		return fmt.Errorf("failed to upload media to WhatsApp: %w", err)
	}
	log.Printf("[template-header-media] Successfully uploaded media, got ID: %s", mediaID)

	return uc.templateRepo.UpdateHeaderMedia(input.TemplateID, input.HeaderMediaURL, &mediaID)
}

func (uc *setTemplateHeaderMediaUseCase) readMedia(key, url string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, storedType, err := uc.files.DownloadFile(ctx, key)
	if err != nil {
		return nil, "", err
	}
	if len(data) > media.MaxReadBytes {
		return nil, "", media.ErrMediaTooLarge
	}

	mimeType := http.DetectContentType(data)

	if mimeType == "application/octet-stream" || mimeType == "" {
		if storedType != "" && storedType != "application/octet-stream" {
			mimeType = storedType
		}
	}

	if mimeType == "application/octet-stream" || mimeType == "" {
		mimeType = inferMimeTypeFromURL(url)
	}

	return data, mimeType, nil
}

func inferMimeTypeFromURL(url string) string {
	url = strings.ToLower(url)
	if idx := strings.Index(url, "?"); idx != -1 {
		url = url[:idx]
	}

	switch {
	case strings.HasSuffix(url, ".jpg"), strings.HasSuffix(url, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(url, ".png"):
		return "image/png"
	case strings.HasSuffix(url, ".webp"):
		return "image/webp"
	case strings.HasSuffix(url, ".gif"):
		return "image/gif"
	case strings.HasSuffix(url, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(url, ".mov"):
		return "video/quicktime"
	case strings.HasSuffix(url, ".avi"):
		return "video/x-msvideo"
	case strings.HasSuffix(url, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(url, ".doc"):
		return "application/msword"
	case strings.HasSuffix(url, ".docx"):
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}
