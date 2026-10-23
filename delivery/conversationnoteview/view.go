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
	Images     []Image   `json:"images"`
	CreatedAt  time.Time `json:"created_at" example:"2026-10-18T12:00:00Z"`
}

type Image struct {
	ID        string `json:"id" example:"3f2a1b0c-9d8e-4f7a-8b6c-5d4e3f2a1b0c"`
	URL       string `json:"url" example:"https://cdn.exemplo.com/conversations/whatsapp/8a3c2f4e/3f2a1b0c.png"`
	MimeType  string `json:"mime_type" example:"image/png"`
	Width     int    `json:"width,omitempty" example:"1280"`
	Height    int    `json:"height,omitempty" example:"720"`
	Thumbhash string `json:"thumbhash,omitempty" example:"1QcSHQRnh493V4dIh4eXh1h4kJUI"`
}

func ImageOf(img conversationnote.Image) Image {
	return Image{
		ID:        img.ID,
		URL:       img.URL,
		MimeType:  img.MimeType,
		Width:     img.Layout.Width,
		Height:    img.Layout.Height,
		Thumbhash: img.Layout.Thumbhash,
	}
}

func Of(n conversationnote.Authored) Note {
	mentions := n.Mentions
	if mentions == nil {
		mentions = []string{}
	}
	images := make([]Image, 0, len(n.Images))
	for _, img := range n.Images {
		images = append(images, ImageOf(img))
	}
	return Note{
		ID:         n.ID,
		EntryID:    n.EntryID,
		EntryType:  string(n.EntryType),
		AuthorID:   n.AuthorID,
		AuthorName: n.AuthorName,
		Body:       n.Body,
		Mentions:   mentions,
		Images:     images,
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
