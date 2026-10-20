package channel

import (
	"mime"
	"strings"
	"time"

	"vozko/domain/shared"
)

var ()

type Kind string

const (
	KindInstagram          Kind = "instagram"
	KindTelegram           Kind = "telegram"
	KindUnofficialWhatsApp Kind = "unofficial_whatsapp"
	KindFacebook           Kind = "facebook"
	KindWebchat            Kind = "webchat"
	KindMercadoLivre       Kind = "mercadolivre"
	KindTikTok             Kind = "tiktok"

	KindWhatsApp Kind = "whatsapp"
)

type Capabilities struct {
	CanInitiateConversation bool

	SupportsTemplates       bool
	SupportsReactions       bool
	SupportsTypingIndicator bool
	SupportsReadReceipts    bool
	SupportsRichText        bool

	MaxTextBytes int
	MaxTextRunes int

	ProviderTextRules []TextRule

	OutboundWindow   time.Duration
	ExtendedWindow   time.Duration
	HumanAgentWindow bool

	OutboundBudget int

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

func (l MediaLimit) Allows(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.Contains(mediaType, "/") {
		return false
	}
	if len(l.MIMETypes) == 0 {
		return true
	}
	for _, m := range l.MIMETypes {
		if strings.EqualFold(m, mediaType) {
			return true
		}
	}
	return false
}

func (c Capabilities) TextTooLong(body string) bool {
	for _, rule := range c.lengthRules() {
		if _, broken := rule.Check(body); broken {
			return true
		}
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
