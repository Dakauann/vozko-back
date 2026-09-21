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

type Kind string

const (
	KindInstagram          Kind = "instagram"
	KindTelegram           Kind = "telegram"
	KindUnofficialWhatsApp Kind = "unofficial_whatsapp"

	KindWhatsApp Kind = "whatsapp"
	KindSupport  Kind = "support"
)

func (k Kind) String() string { return string(k) }

type Capabilities struct {
	CanInitiateConversation bool

	SupportsTemplates       bool
	SupportsReactions       bool
	SupportsTypingIndicator bool
	SupportsReadReceipts    bool
	SupportsRichText        bool

	MaxTextBytes int
	MaxTextRunes int

	OutboundWindow time.Duration
	ExtendedWindow time.Duration

	SignatureFormat string

	MediaLimits map[MediaKind]MediaLimit

	Interactive InteractiveLimits
}

type InteractiveLimits struct {
	MaxOptionsButtons int
	MaxOptionsList    int

	MaxLabelRunes int

	MaxPayloadBytes int

	SupportsOptionDescriptions bool
}

func (l InteractiveLimits) PresentsChoices() bool {
	return l.MaxOptionsButtons > 0 || l.MaxOptionsList > 0
}

func (l InteractiveLimits) MaxOptionsFor(style string) int {
	if style == InteractiveStyleList {
		return l.MaxOptionsList
	}
	return l.MaxOptionsButtons
}

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
	MaxBytes  int64
	MIMETypes []string
}

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

func (c Capabilities) TextTooLong(body string) bool {
	if c.MaxTextBytes > 0 && len(body) > c.MaxTextBytes {
		return true
	}
	if c.MaxTextRunes > 0 && utf8.RuneCountInString(body) > c.MaxTextRunes {
		return true
	}
	return false
}

type InboxSQL struct {
	EntryTable string

	ContactIDField     string
	AccountIDField     string
	ContainerIDField   string
	ContainerNameField string
	AutomationFields   string

	EntryJoin string
}

type Descriptor struct {
	Kind         Kind
	EntryType    shared.EntryType
	Capabilities Capabilities
	InboxSQL     InboxSQL
}

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
