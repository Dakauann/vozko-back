package medias

import (
	"time"

	mediadomain "vozko/domain/media"
)

type MediaResponse struct {
	ID          string    `json:"id" example:"med_a1b2c3"`
	Description string    `json:"description" example:"Música de espera institucional"`
	URL         string    `json:"url" example:"https://cdn.example.com/medias/a1b2c3.mp3"`
	PreviewURL  string    `json:"previewUrl,omitempty" example:"https://cdn.example.com/medias/a1b2c3_preview.jpg"`
	CreatedAt   time.Time `json:"createdAt"`
	Type        string    `json:"type" example:"hold_music"`
}

type MediaDeletedResponse struct {
	Deleted string `json:"deleted" example:"med_a1b2c3"`
}

func toMediaResponse(m mediadomain.Media) MediaResponse {
	return MediaResponse{
		ID:          m.ID,
		Description: m.Description,
		URL:         m.URL,
		PreviewURL:  m.PreviewURL,
		CreatedAt:   m.CreatedAt,
		Type:        string(m.Type),
	}
}

func toMediaResponsePtr(m *mediadomain.Media) *MediaResponse {
	if m == nil {
		return nil
	}
	r := toMediaResponse(*m)
	return &r
}

func toMediaResponses(items []mediadomain.Media) []MediaResponse {
	if items == nil {
		return nil
	}
	out := make([]MediaResponse, len(items))
	for i := range items {
		out[i] = toMediaResponse(items[i])
	}
	return out
}
