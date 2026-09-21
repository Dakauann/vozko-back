package unofficial_whatsapp_campaign

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

var (
	ErrMessageKindInvalid     = errors.New("unofficial whatsapp campaign: unsupported message kind")
	ErrMessageBodyRequired    = errors.New("unofficial whatsapp campaign: the message needs at least one body")
	ErrMessageBodyEmpty       = errors.New("unofficial whatsapp campaign: a message body cannot be empty")
	ErrMessageBodyTooLong     = errors.New("unofficial whatsapp campaign: the message body is too long")
	ErrMessageMediaRequired   = errors.New("unofficial whatsapp campaign: this message kind needs an attachment")
	ErrMessageVariantMismatch = errors.New("unofficial whatsapp campaign: every message variant must use the same variables")
	ErrMenuOptionsRequired    = errors.New("unofficial whatsapp campaign: a menu needs at least one option")
	ErrMenuOptionsTooMany     = errors.New("unofficial whatsapp campaign: too many options for this menu style")
	ErrMenuOptionLabelTooLong = errors.New("unofficial whatsapp campaign: a menu option label is too long")
	ErrMenuOptionIDRequired   = errors.New("unofficial whatsapp campaign: every menu option needs an id")
)

type MessageKind string

const (
	KindText     MessageKind = "text"
	KindImage    MessageKind = "image"
	KindVideo    MessageKind = "video"
	KindAudio    MessageKind = "audio"
	KindDocument MessageKind = "document"
	KindMenu     MessageKind = "menu"
)

func (k MessageKind) Valid() bool {
	switch k {
	case KindText, KindImage, KindVideo, KindAudio, KindDocument, KindMenu:
		return true
	default:
		return false
	}
}

func (k MessageKind) NeedsMedia() bool {
	switch k {
	case KindImage, KindVideo, KindAudio, KindDocument:
		return true
	default:
		return false
	}
}

func (k MessageKind) MediaKind() uw.MediaKind {
	switch k {
	case KindImage:
		return uw.MediaImage
	case KindVideo:
		return uw.MediaVideo
	case KindAudio:
		return uw.MediaVoice
	case KindDocument:
		return uw.MediaDocument
	default:
		return uw.MediaNone
	}
}

const MaxBodyVariants = 10

type MessageSpec struct {
	Kind MessageKind `json:"kind"`

	Bodies []string `json:"bodies"`

	MediaID  string `json:"mediaId,omitempty"`
	FileName string `json:"fileName,omitempty"`

	Style   string                 `json:"style,omitempty"`
	Footer  string                 `json:"footer,omitempty"`
	Button  string                 `json:"button,omitempty"`
	Options []uw.InteractiveOption `json:"options,omitempty"`
}

func (m *MessageSpec) Normalize() {
	m.Kind = MessageKind(strings.ToLower(strings.TrimSpace(string(m.Kind))))
	if m.Kind == "" {
		m.Kind = KindText
	}
	m.MediaID = strings.TrimSpace(m.MediaID)
	m.FileName = strings.TrimSpace(m.FileName)
	m.Style = strings.ToLower(strings.TrimSpace(m.Style))
	m.Footer = strings.TrimSpace(m.Footer)
	m.Button = strings.TrimSpace(m.Button)

	m.Bodies = shared.NonEmptyTrimmed(m.Bodies)

	if m.Kind == KindMenu && m.Style == "" {
		m.Style = uw.InteractiveStyleButtons
	}
}

func (m MessageSpec) ParameterCount() int {
	return shared.HighestPositionalParameter(m.Bodies)
}

func (m MessageSpec) Validate() error {
	if !m.Kind.Valid() {
		return ErrMessageKindInvalid
	}

	captionOptional := m.Kind.NeedsMedia()
	if len(m.Bodies) == 0 && !captionOptional {
		return ErrMessageBodyRequired
	}
	if len(m.Bodies) > MaxBodyVariants {
		return fmt.Errorf("%w: at most %d", ErrMessageVariantMismatch, MaxBodyVariants)
	}

	for _, body := range m.Bodies {
		if strings.TrimSpace(body) == "" {
			return ErrMessageBodyEmpty
		}
		if uw.TextTooLong(body) {
			return ErrMessageBodyTooLong
		}
	}

	if !shared.PositionalParametersAgree(m.Bodies) {
		return ErrMessageVariantMismatch
	}

	if m.Kind.NeedsMedia() && m.MediaID == "" {
		return ErrMessageMediaRequired
	}

	if m.Kind == KindMenu {
		if len(m.Options) == 0 {
			return ErrMenuOptionsRequired
		}
		if len(m.Options) > uw.MaxOptionsFor(m.Style) {
			return ErrMenuOptionsTooMany
		}
		for _, opt := range m.Options {
			if strings.TrimSpace(opt.ID) == "" {
				return ErrMenuOptionIDRequired
			}
			if len([]rune(opt.Title)) > uw.MaxOptionLabelRunes {
				return ErrMenuOptionLabelTooLong
			}
		}
	}

	return nil
}

func (m MessageSpec) VariantFor(entryID string) int {
	return shared.VariantIndexFor(entryID, len(m.Bodies))
}

func (m MessageSpec) Render(variant int, vars []string) string {
	if len(m.Bodies) == 0 {
		return ""
	}
	if variant < 0 || variant >= len(m.Bodies) {
		variant = 0
	}
	return shared.RenderPositional(m.Bodies[variant], vars)
}
