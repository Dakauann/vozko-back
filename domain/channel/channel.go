// Package channel holds the channel-agnostic vocabulary the conversation layer
// uses to talk to a messaging provider without knowing which one it is.
//
// Today only Instagram is registered here. WhatsApp and the support widget still
// carry their behaviour in the per-channel `switch entryType` statements spread
// through usecases/conversation and infra/repositories/conversation (33 sites at
// the time of writing). The migration plan is to move one channel at a time into
// a Descriptor + ChannelAdapter pair until those switches disappear:
//
//  1. Instagram lands here first, greenfield, so the abstraction is proven
//     against a real second channel instead of being designed in the abstract.
//  2. WhatsApp follows by lifting the `case "whatsapp"` bodies verbatim into a
//     whatsappChannelAdapter, behaviour-preserving, so its existing tests must
//     pass untouched.
//  3. The support widget follows.
//  4. Only then do the switches get deleted.
//
// Nothing in this package may import infra or usecases; it is pure domain.
package channel

import (
	"errors"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

var (
	ErrUnknownKind      = errors.New("channel: unknown kind")
	ErrUnknownEntryType = errors.New("channel: unknown entry type")
	ErrKindAlreadySet   = errors.New("channel: kind already registered")
)

// Kind identifies a messaging channel. The string value is persisted (it is the
// same value stored in conversation_messages.channel), so never rename one
// without a data migration.
type Kind string

const (
	KindInstagram Kind = "instagram"
	KindTelegram  Kind = "telegram"

	// Registered here for reference so callers can reason about the full set
	// while the migration is in flight. These are NOT yet backed by a
	// Descriptor, Registry.Get returns ErrUnknownKind for them until their
	// migration step lands.
	KindWhatsApp Kind = "whatsapp"
	KindSupport  Kind = "support"
)

func (k Kind) String() string { return string(k) }

// Capabilities describes what a channel can do, so callers branch on a
// capability rather than on a Kind. Adding a channel must not require editing
// a switch somewhere else.
type Capabilities struct {
	// CanInitiateConversation reports whether we may send the first message.
	// Instagram: false, a user must message the account first, and there is no
	// template-initiated conversation as there is on WhatsApp.
	CanInitiateConversation bool

	SupportsTemplates       bool
	SupportsReactions       bool
	SupportsTypingIndicator bool
	SupportsReadReceipts    bool
	// SupportsRichText reports whether the provider renders markup. Instagram
	// DMs are plain text, so the operator signature must not be wrapped in
	// WhatsApp's *bold* syntax.
	SupportsRichText bool

	// MaxTextBytes is a BYTE limit, not a rune count. Instagram documents
	// "1000 bytes or less" and multibyte emoji blow a naive character count.
	//
	// Exactly one of MaxTextBytes / MaxTextRunes is set per channel. Zero means
	// "this channel does not measure text that way"; TextTooLong applies whichever
	// is set, so a channel cannot be silently unbounded by picking the wrong one.
	MaxTextBytes int
	// MaxTextRunes is a CHARACTER limit. Telegram documents "1-4096 characters
	// after entities parsing", which is a different measurement from Instagram's
	// byte cap: conflating them truncates emoji-heavy text on one channel and
	// over-accepts on the other.
	MaxTextRunes int

	// OutboundWindow is how long after the contact's last inbound message we
	// may reply. Zero means unlimited.
	OutboundWindow time.Duration
	// ExtendedWindow is the longer window available under a provider-specific
	// escalation (Instagram: the human_agent tag, 7 days). Zero means none.
	ExtendedWindow time.Duration

	// SignatureFormat renders an operator-signed message. Two verbs: username,
	// then body.
	SignatureFormat string

	// Media constraints, keyed by the normalized media kind.
	MediaLimits map[MediaKind]MediaLimit

	// Interactive describes how this channel presents a single-choice prompt.
	// The zero value means the channel cannot present one at all.
	Interactive InteractiveLimits
}

// InteractiveLimits describes a channel's "pick one option" support.
//
// Every channel does this differently, and the differences are visible to the
// person authoring a workflow: WhatsApp splits at three options into a
// different message type, Instagram truncates labels at twenty characters,
// Telegram caps the machine-readable payload rather than the option count. The
// workflow editor reads these to tell an author, before they publish, exactly
// which options each connected channel will render.
type InteractiveLimits struct {
	// MaxOptionsButtons and MaxOptionsList are separate because some channels
	// present a short button row and a longer scrollable list as two different
	// message types (WhatsApp: 3 and 10). Channels with a single mechanism that
	// serves both, Instagram quick replies, Telegram inline keyboards, set
	// both fields to the same number.
	//
	// Zero in both means the channel cannot present a choice.
	MaxOptionsButtons int
	MaxOptionsList    int

	// MaxLabelRunes bounds the visible option label. Zero means the provider
	// documents no limit, not that the limit is unknown-and-ignored.
	MaxLabelRunes int

	// MaxPayloadBytes bounds the machine-readable option id that comes back
	// when the option is pressed. It is a BYTE limit: Telegram's callback_data
	// is documented as "1-64 bytes", and an option id with accented characters
	// or an emoji overflows it well before 64 visible characters.
	MaxPayloadBytes int

	// SupportsOptionDescriptions reports whether an option can carry a second
	// line of explanatory text under its label.
	//
	// Only WhatsApp's list rows have that slot. Telegram inline buttons and
	// Instagram quick replies are label-only, so a description written by an
	// author is simply not shown there, which the editor must say out loud,
	// because the author cannot tell from the config panel alone.
	SupportsOptionDescriptions bool
}

// PresentsChoices reports whether the channel can ask the contact to pick one.
func (l InteractiveLimits) PresentsChoices() bool {
	return l.MaxOptionsButtons > 0 || l.MaxOptionsList > 0
}

// MaxOptionsFor returns the option cap for a given prompt style. Anything that
// is not the list style is treated as the button style, so an unrecognised
// value fails toward the smaller, safer number.
func (l InteractiveLimits) MaxOptionsFor(style string) int {
	if style == InteractiveStyleList {
		return l.MaxOptionsList
	}
	return l.MaxOptionsButtons
}

// The two prompt styles an author can pick. They map onto WhatsApp's two
// interactive message types; other channels render both with their single
// native mechanism.
const (
	InteractiveStyleButtons = "buttons"
	InteractiveStyleList    = "list"
)

type MediaKind string

const (
	MediaImage    MediaKind = "image"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaDocument MediaKind = "document"
)

type MediaLimit struct {
	MaxBytes int64
	// MIMETypes is the accepted set. A nil/empty slice means the channel accepts
	// ANY type for this kind, Telegram's sendDocument does, unlike Instagram's
	// PDF-only file attachment. Treating empty as "accept nothing" would reject
	// every document on such a channel.
	MIMETypes []string
}

// Allows reports whether mime is accepted for this kind.
func (l MediaLimit) Allows(mime string) bool {
	if len(l.MIMETypes) == 0 {
		return true
	}
	for _, m := range l.MIMETypes {
		if m == mime {
			return true
		}
	}
	return false
}

// TextTooLong reports whether body exceeds this channel's text limit, using
// whichever measurement the channel actually documents.
//
// This exists so no caller has to remember that Instagram counts bytes and
// Telegram counts characters, getting it wrong is invisible until a customer
// sends an emoji-heavy message.
func (c Capabilities) TextTooLong(body string) bool {
	if c.MaxTextBytes > 0 && len(body) > c.MaxTextBytes {
		return true
	}
	if c.MaxTextRunes > 0 && utf8.RuneCountInString(body) > c.MaxTextRunes {
		return true
	}
	return false
}

// InboxSQL carries the per-channel SQL fragments the inbox query needs. It
// exists so infra/repositories/conversation stops hardcoding one channel's
// tables; see the `switch entryType` that builds these today.
//
// These are trusted, code-authored constants, never user input, and are
// interpolated into the query text. Bind parameters still carry all values.
type InboxSQL struct {
	// EntryTable is the table holding the entry (== the conversation) and its
	// denormalized last_message_at.
	EntryTable string

	// ContactIDField yields the contact primary key.
	ContactIDField string
	// AccountIDField yields the channel account the conversation belongs to
	// (WhatsApp: business phone; Instagram: instagram account).
	AccountIDField string
	// ContainerIDField / ContainerNameField yield the config-carrying parent
	// shown as the conversation's origin.
	ContainerIDField   string
	ContainerNameField string
	// AutomationFields yields agent_id, workflow_id, agent_responses_enabled
	// and workflow_enabled, aliased exactly so.
	AutomationFields string

	// EntryJoin joins the entry (and its container) onto the message alias.
	// It must expose the aliases used by the field expressions above.
	EntryJoin string
}

// Descriptor is the single registration point for a channel.
type Descriptor struct {
	Kind         Kind
	EntryType    shared.EntryType
	Capabilities Capabilities
	InboxSQL     InboxSQL
}

// Registry resolves descriptors. Implementations must be safe for concurrent
// reads after construction.
type Registry interface {
	Get(k Kind) (*Descriptor, error)
	ByEntryType(t shared.EntryType) (*Descriptor, error)
	All() []*Descriptor
}

type registry struct {
	byKind      map[Kind]*Descriptor
	byEntryType map[shared.EntryType]*Descriptor
	ordered     []*Descriptor
}

// NewRegistry builds an immutable registry. It returns an error rather than
// panicking so the container surfaces a misconfiguration at boot.
func NewRegistry(descriptors ...*Descriptor) (Registry, error) {
	r := &registry{
		byKind:      make(map[Kind]*Descriptor, len(descriptors)),
		byEntryType: make(map[shared.EntryType]*Descriptor, len(descriptors)),
		ordered:     make([]*Descriptor, 0, len(descriptors)),
	}
	for _, d := range descriptors {
		if d == nil {
			continue
		}
		if _, exists := r.byKind[d.Kind]; exists {
			return nil, ErrKindAlreadySet
		}
		r.byKind[d.Kind] = d
		r.byEntryType[d.EntryType] = d
		r.ordered = append(r.ordered, d)
	}
	return r, nil
}

func (r *registry) Get(k Kind) (*Descriptor, error) {
	if d, ok := r.byKind[k]; ok {
		return d, nil
	}
	return nil, ErrUnknownKind
}

func (r *registry) ByEntryType(t shared.EntryType) (*Descriptor, error) {
	if d, ok := r.byEntryType[t]; ok {
		return d, nil
	}
	return nil, ErrUnknownEntryType
}

func (r *registry) All() []*Descriptor {
	out := make([]*Descriptor, len(r.ordered))
	copy(out, r.ordered)
	return out
}
