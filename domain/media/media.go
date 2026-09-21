package media

import (
	"errors"
	"time"
)

var ErrMediaNotFound = errors.New("media not found")

type MediaType string

const (
	MediaTypeVslVideo     MediaType = "vsl_video"
	MediaTypeProductImage MediaType = "image"
	MediaTypeProductVideo MediaType = "video"
	MediaTypeDocumentPdf  MediaType = "document_pdf"
	MediaTypeDocumentDoc  MediaType = "document_doc"
	MediaTypeHtml5        MediaType = "html5"
	MediaTypeAudio        MediaType = "audio"
	MediaTypeDocument     MediaType = "document"
	MediaTypeSticker      MediaType = "sticker"
)

type Media struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	WorkspaceID string    `json:"-"`
	URL         string    `json:"url"`
	PreviewURL  string    `json:"previewUrl,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	Type        MediaType `json:"type"`
}
