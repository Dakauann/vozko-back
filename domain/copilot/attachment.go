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
	Content       string
	AttachmentIDs []string
}

type Attachment struct {
	MediaID string `json:"mediaId"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
}

func PromptWithAttachments(content string, attachments []Attachment) string {
	if len(attachments) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n[Arquivos anexados pelo usuário nesta mensagem; use o media_id nas ferramentas]")
	for _, a := range attachments {
		b.WriteString("\n- " + a.Name + " (media_id: " + a.MediaID + ", tipo: " + a.Kind + ")")
	}
	return strings.TrimSpace(b.String())
}
