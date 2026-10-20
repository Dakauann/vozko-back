package conversationnoteview

import (
	"time"

	"vozko/domain/conversationnote"
)

type Note struct {
	ID         string    `json:"id" example:"5b8f7a0e-2d7c-4f6e-9a51-0c3b7e2f9d10"`
	EntryID    string    `json:"entry_id" example:"8a3c2f4e-1b5d-4c6a-9e7f-2d1b3c4a5e6f"`
	EntryType  string    `json:"entry_type" example:"whatsapp"`
	AuthorID   string    `json:"author_id" example:"0e9d8c7b-6a5f-4e3d-2c1b-0a9f8e7d6c5b"`
	AuthorName string    `json:"author_name" example:"Carla"`
	Body       string    `json:"body" example:"Cliente pediu desconto, aguardando o gerente."`
	Mentions   []string  `json:"mentions"`
	CreatedAt  time.Time `json:"created_at" example:"2026-10-18T12:00:00Z"`
}

func Of(n conversationnote.Authored) Note {
	mentions := n.Mentions
	if mentions == nil {
		mentions = []string{}
	}
	return Note{
		ID:         n.ID,
		EntryID:    n.EntryID,
		EntryType:  string(n.EntryType),
		AuthorID:   n.AuthorID,
		AuthorName: n.AuthorName,
		Body:       n.Body,
		Mentions:   mentions,
		CreatedAt:  n.CreatedAt,
	}
}

func ListOf(notes []conversationnote.Authored) []Note {
	out := make([]Note, 0, len(notes))
	for _, n := range notes {
		out = append(out, Of(n))
	}
	return out
}
