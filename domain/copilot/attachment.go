package copilot

import (
	"errors"
	"strings"
)

const MaxAttachments = 5

var (
	ErrTooManyAttachments = errors.New("copilot: too many attachments in one message")
	ErrAttachmentNotFound = errors.New("copilot: attachment not found in this workspace")
)

type UserMessage struct {
	ClientID      string
	Content       string
	AttachmentIDs []string
}

type Attachment struct {
	MediaID string `json:"mediaId"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
}

const imageKind = "image"

func ImageURLs(attachments []Attachment) []string {
	var out []string
	for _, a := range attachments {
		if a.Kind == imageKind && strings.TrimSpace(a.URL) != "" {
			out = append(out, a.URL)
		}
	}
	return out
}

const BlindImagesNote = "[O modelo desta conversa não enxerga imagens: as imagens anexadas não chegam até você. Não descreva nem suponha o que elas mostram; diga ao usuário que com este modelo você não consegue ver imagens e sugira trocar, no seletor de modelos, para um modelo com visão]"

func PromptWithAttachments(content string, attachments []Attachment, seesImages bool) string {
	if len(attachments) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n[Arquivos anexados pelo usuário nesta mensagem; use o media_id nas ferramentas]")
	for _, a := range attachments {
		b.WriteString("\n- " + a.Name + " (media_id: " + a.MediaID + ", tipo: " + a.Kind + ")")
	}
	if !seesImages && len(ImageURLs(attachments)) > 0 {
		b.WriteString("\n" + BlindImagesNote)
	}
	return strings.TrimSpace(b.String())
}
