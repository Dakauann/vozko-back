package unofficial_whatsapp

import (
	"context"
	"strings"
	"unicode/utf8"
)

type SendTextInput struct {
	ChatID                   string
	Text                     string
	ReplyToProviderMessageID string

	DelayMS int

	TrackSource string
	TrackID     string
}

type SendMediaInput struct {
	ChatID   string
	Kind     MediaKind
	URL      string
	Base64   string
	MIMEType string
	FileName string
	Caption  string

	ReplyToProviderMessageID string
	DelayMS                  int
	TrackSource              string
	TrackID                  string
}

type InteractiveOption struct {
	ID          string
	Title       string
	Description string
}

type SendMenuInput struct {
	ChatID  string
	Style   string
	Body    string
	Footer  string
	Button  string
	Options []InteractiveOption

	DelayMS     int
	TrackSource string
	TrackID     string
}

type SendResult struct {
	ProviderMessageID string
	Status            DeliveryStatus
}

type RemoteMedia struct {
	Data     []byte
	MIMEType string
	FileName string
	URL      string
}

type NumberCheck struct {
	Query        string
	JID          string
	LID          string
	IsOnWhatsApp bool
	VerifiedName string
}

type ChatProfile struct {
	JID          string
	LID          string
	Name         string
	ContactName  string
	VerifiedName string
	PictureURL   string
	PhoneNumber  string
	IsBusiness   bool
	IsGroup      bool
	IsBlocked    bool
}

type VoiceTranscoder interface {
	ToVoiceNote(ctx context.Context, url string) ([]byte, error)
}

const MaxAvatarBytes = 8 << 20

type RemoteAssetFetcher interface {
	FetchAsset(ctx context.Context, url string) (data []byte, contentType string, err error)
}

type Presence string

const (
	PresenceTyping    Presence = "composing"
	PresenceRecording Presence = "recording"
	PresencePaused    Presence = "paused"
)

const MaxPresenceMS = 300000

type MessagingAPI interface {
	SendText(ctx context.Context, ref InstanceRef, in SendTextInput) (*SendResult, error)
	SendMedia(ctx context.Context, ref InstanceRef, in SendMediaInput) (*SendResult, error)
	SendMenu(ctx context.Context, ref InstanceRef, in SendMenuInput) (*SendResult, error)

	SendPresence(ctx context.Context, ref InstanceRef, chatID string, presence Presence, delayMS int) error
	MarkRead(ctx context.Context, ref InstanceRef, providerMessageIDs []string) error
	React(ctx context.Context, ref InstanceRef, chatID, providerMessageID, emoji string) error
	EditMessage(ctx context.Context, ref InstanceRef, providerMessageID, text string) (*SendResult, error)
	DeleteMessage(ctx context.Context, ref InstanceRef, providerMessageID string) error

	DownloadMedia(ctx context.Context, ref InstanceRef, providerMessageID string) (*RemoteMedia, error)
	ChatDetails(ctx context.Context, ref InstanceRef, chatID string) (*ChatProfile, error)
	CheckNumbers(ctx context.Context, ref InstanceRef, numbers []string) ([]NumberCheck, error)
}

const placeholderOpen = "{{"

func SanitizeOutboundText(body string) string {
	if !strings.Contains(body, placeholderOpen) {
		return body
	}
	return strings.ReplaceAll(body, placeholderOpen, "{​{")
}

func TextTooLong(body string) bool {
	return utf8.RuneCountInString(body) > MaxTextRunes
}

const (
	InteractiveStyleButtons = "buttons"
	InteractiveStyleList    = "list"
)

func MaxOptionsFor(style string) int {
	if style == InteractiveStyleList {
		return MaxListOptions
	}
	return MaxButtonOptions
}
