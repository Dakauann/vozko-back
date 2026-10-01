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
	Layout           MediaLayout      `json:"layout"`
	CreatedAt        time.Time        `json:"createdAt"`
}

type MediaLayout struct {
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Thumbhash string `json:"thumbhash,omitempty"`
}

func (l MediaLayout) Known() bool {
	return l.Width > 0 && l.Height > 0
}

type MediaInspector interface {
	Inspect(data []byte, mediaType MediaType) MediaLayout
}

type StoreMediaInput struct {
	ID               string
	Key              string
	EntryID          string
	EntryType        shared.EntryType
	Type             MediaType
	MimeType         string
	Data             []byte
	OriginalFilename string
	WhatsAppMediaID  string
}

type MediaStore interface {
	Store(input StoreMediaInput) (*ConversationMedia, error)
}

type AttachedMedia struct {
	URL      string      `json:"url"`
	MimeType string      `json:"mimeType,omitempty"`
	Filename string      `json:"filename,omitempty"`
	Layout   MediaLayout `json:"layout"`
}

func (m *ConversationMedia) Attachment() *AttachedMedia {
	return &AttachedMedia{URL: m.URL, MimeType: m.MimeType, Filename: m.OriginalFilename, Layout: m.Layout}
}

func (m *ConversationMedia) BelongsTo(entryID string, entryType shared.EntryType) bool {
	return m != nil && m.EntryID == entryID && m.EntryType == entryType
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
	ListByIDs(ids []string) ([]*ConversationMedia, error)
	GetByWhatsAppMediaID(whatsappMediaID string) (*ConversationMedia, error)
	ListByEntry(entryID string, entryType shared.EntryType) ([]*ConversationMedia, error)
	Delete(id string) error
	DeleteByEntry(entryID string, entryType shared.EntryType) error
}
