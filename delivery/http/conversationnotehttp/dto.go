package conversationnotehttp

import "vozko/delivery/conversationnoteview"

type CreateNoteRequest struct {
	Body     string   `json:"body" example:"@Ana o cliente pediu desconto, pode aprovar?"`
	Mentions []string `json:"mentions,omitempty"`
	ImageIDs []string `json:"image_ids,omitempty"`
}

type NoteListResponse struct {
	Notes   []conversationnoteview.Note `json:"notes"`
	HasMore bool                        `json:"has_more"`
}

type NoteEnvelope struct {
	Note conversationnoteview.Note `json:"note"`
}

type NoteImageEnvelope struct {
	Image conversationnoteview.Image `json:"image"`
}
