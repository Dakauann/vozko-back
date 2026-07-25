package media

import (
	"time"
)

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
	// MediaTypeHoldMusic is a workspace hold music track. Unlike plain audio it is
	// STANDARDIZED at upload: transcoded to a small mono MP3 sized for telephony
	// (the call media plane is 8kHz G.711), so the boot-style MP3 loader can decode
	// it and a 25MB podcast can never sit on the hold path.
	MediaTypeHoldMusic MediaType = "hold_music"
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
