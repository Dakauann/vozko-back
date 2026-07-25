package message_shortcut

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type MessageType string

const (
	MessageTypeText   MessageType = "text"
	MessageTypeButton MessageType = "button"
	MessageTypeMedia  MessageType = "media"
)

type MediaType string

const (
	MediaTypeImage    MediaType = "image"
	MediaTypeVideo    MediaType = "video"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeDocument MediaType = "document"
)

type MessageShortcut struct {
	ID          string      `json:"id"`
	WorkspaceID string      `json:"workspaceId"`
	Shortcut    string      `json:"shortcut"`
	Name        string      `json:"name"`
	MessageType MessageType `json:"messageType"`
	Content     Content     `json:"content"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

type Content struct {
	Text       string   `json:"text,omitempty"`
	HeaderType string   `json:"headerType,omitempty"`
	HeaderText string   `json:"headerText,omitempty"`
	FooterText string   `json:"footerText,omitempty"`
	MediaURL   string   `json:"mediaUrl,omitempty"`
	MediaType  string   `json:"mediaType,omitempty"`
	Buttons    []Button `json:"buttons,omitempty"`
}

type Button struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (c *Content) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal Content value")
	}
	if len(bytes) == 0 {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

func (c Content) Value() (driver.Value, error) {
	return json.Marshal(c)
}

var (
	ErrNotFound          = errors.New("message shortcut not found")
	ErrNameRequired      = errors.New("name is required")
	ErrShortcutRequired  = errors.New("shortcut is required")
	ErrShortcutExists    = errors.New("shortcut already exists in this workspace")
	ErrTextRequired      = errors.New("text content is required")
	ErrInvalidType       = errors.New("message type must be text, button, or media")
	ErrButtonsRequired   = errors.New("button message requires at least one button")
	ErrTooManyButtons    = errors.New("maximum 3 buttons allowed")
	ErrMediaURLRequired  = errors.New("media message requires a media URL")
	ErrShortcutNoSpaces  = errors.New("shortcut must not contain spaces")
	ErrShortcutMaxLength = errors.New("shortcut must be at most 50 characters")
)

func (s *MessageShortcut) Validate() error {
	s.Shortcut = strings.TrimSpace(strings.ToLower(s.Shortcut))
	s.Name = strings.TrimSpace(s.Name)

	if s.Name == "" {
		return ErrNameRequired
	}
	if s.Shortcut == "" {
		return ErrShortcutRequired
	}
	if strings.Contains(s.Shortcut, " ") {
		return ErrShortcutNoSpaces
	}
	if len(s.Shortcut) > 50 {
		return ErrShortcutMaxLength
	}

	switch s.MessageType {
	case MessageTypeText:
		if strings.TrimSpace(s.Content.Text) == "" {
			return ErrTextRequired
		}
	case MessageTypeButton:
		if strings.TrimSpace(s.Content.Text) == "" {
			return ErrTextRequired
		}
		if len(s.Content.Buttons) == 0 {
			return ErrButtonsRequired
		}
		if len(s.Content.Buttons) > 3 {
			return ErrTooManyButtons
		}
	case MessageTypeMedia:
		if strings.TrimSpace(s.Content.MediaURL) == "" {
			return ErrMediaURLRequired
		}
	default:
		return ErrInvalidType
	}
	return nil
}
