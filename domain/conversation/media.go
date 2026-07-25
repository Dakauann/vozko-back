package conversation

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

type ConversationMedia struct {
	ID               string           `json:"id"`
	EntryID          string           `json:"entryId"`
	EntryType        shared.EntryType `json:"entryType"`
	Type             MediaType        `json:"type"`
	MimeType         string           `json:"mimeType"`
	URL              string           `json:"url"`
	OriginalFilename string           `json:"originalFilename,omitempty"`
	SizeBytes        int64            `json:"sizeBytes,omitempty"`
	DurationSeconds  *int             `json:"durationSeconds,omitempty"`
	WhatsAppMediaID  string           `json:"whatsappMediaId,omitempty"`
	CreatedAt        time.Time        `json:"createdAt"`
}

func (m *ConversationMedia) Normalize() {
	if m == nil {
		return
	}
	m.ID = strings.TrimSpace(m.ID)
	m.EntryID = strings.TrimSpace(m.EntryID)
	m.MimeType = strings.TrimSpace(m.MimeType)
	m.URL = strings.TrimSpace(m.URL)
	m.OriginalFilename = strings.TrimSpace(m.OriginalFilename)
	m.WhatsAppMediaID = strings.TrimSpace(m.WhatsAppMediaID)
}

func (m *ConversationMedia) Validate() error {
	if m == nil {
		return ErrMediaRequired
	}
	if m.EntryID == "" {
		return ErrEntryIDRequired
	}
	if !m.EntryType.Valid() {
		return ErrEntryTypeInvalid
	}
	if !m.Type.Valid() {
		return ErrMediaTypeInvalid
	}
	if m.URL == "" {
		return ErrMediaURLRequired
	}
	return nil
}

type ConversationMediaRepository interface {
	Create(media *ConversationMedia) error
	GetByID(id string) (*ConversationMedia, error)
	GetByWhatsAppMediaID(whatsappMediaID string) (*ConversationMedia, error)
	ListByEntry(entryID string, entryType shared.EntryType) ([]*ConversationMedia, error)
	Delete(id string) error
	DeleteByEntry(entryID string, entryType shared.EntryType) error
}
