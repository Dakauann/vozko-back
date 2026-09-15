// Package unofficial_whatsapp_campaign is bulk outbound over a linked-device
// WhatsApp session.
//
// It is the sibling of whatsapp_campaign and deliberately not a variant of it.
// The two agree on everything an operator sees — the lifecycle, the metrics, the
// entries table, the reset flow, all of which come from domain/campaign — and
// disagree on the two things that decide the shape of a campaign entity:
//
//  1. There is no template. The message is authored inline, so this package
//     carries a MessageSpec where the official one carries a TemplateID.
//  2. There is no cost. Meta charges nothing for a linked-device send, so
//     nothing here reserves, debits or refunds. See the package comment on
//     the consumer for what is built instead so pricing stays addable.
//
// A third difference shapes the entry: on the Cloud API the campaign entry IS
// the conversation, while here one real WhatsApp chat is one CRM conversation
// forever, so an entry POINTS AT a conversation instead of being one.
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

// MessageKind is what one campaign sends.
//
// A closed set that maps 1:1 onto the three send methods uw.MessagingAPI
// exposes, so a kind can never exist that the adapter has no way to deliver.
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

// NeedsMedia reports whether this kind is undeliverable without an attachment.
func (k MessageKind) NeedsMedia() bool {
	switch k {
	case KindImage, KindVideo, KindAudio, KindDocument:
		return true
	default:
		return false
	}
}

// MediaKind maps onto the channel's own attachment vocabulary.
//
// Audio becomes MediaVoice rather than MediaAudio deliberately: a campaign
// audio is a voice note, which is what a person would send, and an audio file
// attachment from a business number reads as a broadcast.
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

// MaxBodyVariants bounds how many rotations one campaign may carry.
//
// A cap rather than unlimited because each variant is a body an operator has to
// proofread, and a campaign with fifty of them is one where nobody has read
// most of what their customers receive.
const MaxBodyVariants = 10

// The positional variable syntax, {{1}} {{2}}, and everything that reads or
// renders it, lives in domain/shared. Not for tidiness: this package imports
// domain/unofficial_whatsapp, so that package cannot import this one, and inbox
// seeding needs the same rules. Lifting beat copying.

// MessageSpec is the campaign's payload — this channel's replacement for a
// template.
type MessageSpec struct {
	Kind MessageKind `json:"kind"`

	// Bodies is a LIST, and that is a ban-avoidance control rather than a
	// convenience.
	//
	// WhatsApp's spam heuristics weight identical bodies leaving one number at
	// volume, and this channel has no template to hide behind. One variant is
	// picked per recipient, so a 5.000-number campaign sends five different
	// texts rather than one text five thousand times. A single-element list is
	// the default and behaves exactly like a plain body.
	Bodies []string `json:"bodies"`

	// MediaID references OUR media store, never a caller-supplied URL: an
	// arbitrary external URL would make the campaign a server-side request
	// forger pointed at whatever the caller likes.
	MediaID  string `json:"mediaId,omitempty"`
	FileName string `json:"fileName,omitempty"`

	// Menu only.
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
		// Buttons is the safer default: WhatsApp renders a list as a menu the
		// contact has to open, and a two-option prompt hidden behind a tap is a
		// prompt most people never answer.
		m.Style = uw.InteractiveStyleButtons
	}
}

// ParameterCount is the highest positional placeholder used by any variant.
//
// The maximum across variants rather than per-variant, because the importer
// collects one set of columns for the whole campaign: a recipient needs enough
// variables for whichever variant they happen to be assigned.
func (m MessageSpec) ParameterCount() int {
	return shared.HighestPositionalParameter(m.Bodies)
}

func (m MessageSpec) Validate() error {
	if !m.Kind.Valid() {
		return ErrMessageKindInvalid
	}

	// A menu carries its prompt in Bodies[0]; media carries a caption there,
	// which may legitimately be empty for an image or a document.
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

	// Every variant must use the SAME placeholders.
	//
	// Not merely the same count: a campaign whose first variant reads {{1}} and
	// whose second reads {{2}} would send a raw "{{2}}" to everyone assigned the
	// second, because the importer only collected one column.
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
				// Workflows branch on the id, never on the label. An option
				// without one is a branch nothing can match.
				return ErrMenuOptionIDRequired
			}
			if len([]rune(opt.Title)) > uw.MaxOptionLabelRunes {
				return ErrMenuOptionLabelTooLong
			}
		}
	}

	return nil
}

// VariantFor picks which body a given entry receives.
//
// Deterministic in the entry id rather than random, and that is what makes the
// feature debuggable: the same recipient always gets the same variant, so a
// resumed or partially-retried campaign never sends one person two different
// messages, and "which text did this customer get" is answerable from the row.
func (m MessageSpec) VariantFor(entryID string) int {
	return shared.VariantIndexFor(entryID, len(m.Bodies))
}

// Render substitutes positional variables into the chosen body.
//
// Rendering happens HERE, in the domain, and the result is passed through
// uw.SanitizeOutboundText by the sender before it reaches the provider. The
// order is load-bearing and asserted by a test: the provider performs its OWN
// {{...}} substitution from ITS lead store, so a brace that survives to the wire
// leaks another tenant's data into a customer's chat.
//
// A placeholder with no matching variable is left as written rather than blanked,
// so a misconfigured campaign is visibly wrong instead of silently sending a
// sentence with a hole in it.
func (m MessageSpec) Render(variant int, vars []string) string {
	if len(m.Bodies) == 0 {
		return ""
	}
	if variant < 0 || variant >= len(m.Bodies) {
		variant = 0
	}
	return shared.RenderPositional(m.Bodies[variant], vars)
}
