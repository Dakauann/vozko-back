package template_usecase

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"vozko/domain/whatsapp/template"
)

type setTemplateHeaderMediaUseCase struct {
	templateRepo  template.Repository
	clientFactory template.WhatsAppClientFactory
	httpClient    *http.Client
}

func NewSetTemplateHeaderMediaUseCase(templateRepo template.Repository, clientFactory template.WhatsAppClientFactory) template.SetTemplateHeaderMediaUseCase {
	return &setTemplateHeaderMediaUseCase{
		templateRepo:  templateRepo,
		clientFactory: clientFactory,
		httpClient:    &http.Client{Timeout: 60 * time.Second},
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

	if tmpl.WABAId == "" {
		return fmt.Errorf("template has no WABA ID — cannot upload media")
	}

	client, err := uc.clientFactory.ClientForWABA(tmpl.WABAId)
	if err != nil {
		return fmt.Errorf("failed to get WhatsApp client for phone: %w", err)
	}

	log.Printf("[template-header-media] Downloading media from URL: %s", *input.HeaderMediaURL)
	mediaData, mimeType, err := uc.downloadMedia(*input.HeaderMediaURL)
	if err != nil {
		return fmt.Errorf("failed to download media from URL: %w", err)
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

func (uc *setTemplateHeaderMediaUseCase) downloadMedia(url string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("failed to download media: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	mimeType := http.DetectContentType(data)

	if mimeType == "application/octet-stream" || mimeType == "" {
		headerType := resp.Header.Get("Content-Type")
		if headerType != "" && headerType != "application/octet-stream" {
			mimeType = headerType
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
