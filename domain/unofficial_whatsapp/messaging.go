package unofficial_whatsapp

import (
	"context"
	"strings"
	"unicode/utf8"
)

// The send and media surface.
//
// Split from ProviderAPI rather than folded into it, because the two are used
// by different callers with different lifetimes: the health cron and the
// provisioning flow need the lifecycle surface and must not be able to send, and
// a test double for either should not have to implement the other.

// SendTextInput is one text send.
type SendTextInput struct {
	// ChatID is the destination: an E.164 number, a JID, or a group id.
	ChatID string
	Text   string
	// ReplyToProviderMessageID quotes an earlier message.
	ReplyToProviderMessageID string

	// DelayMS renders a typing indicator for its duration before the message
	// lands. Human pacing is not decoration on this channel: a burst of
	// instantaneous replies is one of the signals that gets a number banned.
	DelayMS int

	// TrackSource and TrackID are the correlation tag that makes the echo
	// recognisable. Always set — an untagged send comes back looking like the
	// owner typed it on their phone.
	TrackSource string
	TrackID     string
}

// SendMediaInput is one attachment send.
//
// Exactly one of URL or Base64 is used. A URL is preferred: the provider fetches
// it directly, which avoids moving the bytes through our process twice.
type SendMediaInput struct {
	ChatID string
	// Kind maps onto the provider's media types.
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

// InteractiveOption is one choice offered to the contact.
type InteractiveOption struct {
	// ID is the contract: workflows branch on it, never on the label.
	ID    string
	Title string
	// Description is a second line, rendered only by list rows.
	Description string
}

// SendMenuInput is a "pick one" prompt.
type SendMenuInput struct {
	ChatID string
	// Style is buttons or list. WhatsApp sends a different message type for
	// each, with different caps, which is why the caller must choose.
	Style   string
	Body    string
	Footer  string
	Button  string
	Options []InteractiveOption

	DelayMS     int
	TrackSource string
	TrackID     string
}

// SendResult is the provider's acknowledgement of a send.
type SendResult struct {
	// ProviderMessageID is stored as external_message_id, so the echo webhook
	// reconciles against the row we just wrote instead of inserting a duplicate.
	ProviderMessageID string
	Status            DeliveryStatus
}

// RemoteMedia is a downloaded attachment.
type RemoteMedia struct {
	Data     []byte
	MIMEType string
	FileName string
	// URL is the provider's own hosted copy, when it returned one. Never used as
	// the durable asset: a third-party URL is not something we can guarantee
	// still resolves, and it would leak tenant media to anyone holding it.
	URL string
}

// NumberCheck is one result of a WhatsApp registration lookup.
type NumberCheck struct {
	Query        string
	JID          string
	LID          string
	IsOnWhatsApp bool
	VerifiedName string
}

// ChatProfile is the refreshable identity of a chat SUBJECT — a person or a
// group.
//
// One type for both because the provider answers both from one endpoint and the
// CRM asks both the same question. The alternative was two near-identical
// structs and two near-identical fetch paths, which is how the name half and the
// picture half of "who is this" drift apart.
//
// PictureURL is the PROVIDER's URL and is never persisted as-is: WhatsApp's
// avatar links are short-lived and unauthenticated, so the bytes are re-hosted
// before anything stores a link. See the profile sync in the usecase layer.
type ChatProfile struct {
	JID string
	LID string
	// Name is the WhatsApp profile name (the "push name"), or a group's subject.
	Name string
	// ContactName is what the connected phone has this person saved as. It only
	// exists when the number is in the owner's address book, and it outranks
	// every other name because it is what the operator already calls them.
	ContactName string
	// VerifiedName is a WhatsApp Business verified display name.
	VerifiedName string
	PictureURL   string
	PhoneNumber  string
	IsBusiness   bool
	IsGroup      bool
	IsBlocked    bool
}

// VoiceTranscoder turns a stored audio file into a sendable WhatsApp voice note.
//
// A port rather than a helper because both halves of the job are infrastructure:
// fetching the stored file over HTTP and shelling out to ffmpeg. The adapter
// only knows that this channel cannot send anything but ogg/opus — which is what
// its own descriptor already declares — and that it therefore has to convert.
//
// Optional: a deployment without ffmpeg still sends every other media kind, and
// an audio send fails with a message naming the reason rather than the whole
// channel refusing to start.
type VoiceTranscoder interface {
	// ToVoiceNote fetches the audio at url and returns it as ogg/opus bytes.
	ToVoiceNote(ctx context.Context, url string) ([]byte, error)
}

// MaxAvatarBytes bounds what will be pulled into memory for one profile picture.
//
// A WhatsApp avatar is a small square; anything approaching this is not one, and
// an unbounded read of a remote body on the webhook path is an availability bug
// waiting for a bad day.
const MaxAvatarBytes = 8 << 20

// RemoteAssetFetcher fetches bytes from a URL the provider handed us.
//
// A port rather than an inline HTTP call because the usecase layer must not open
// sockets, and because the thing being fetched is provider-hosted: the URL came
// from the vendor, it is short-lived, and only the vendor package should know
// how to reach it.
//
// Optional. Without it, profile enrichment stores names and skips pictures,
// which is a degraded avatar rather than a broken channel.
type RemoteAssetFetcher interface {
	// FetchAsset returns the bytes and the server's content type.
	FetchAsset(ctx context.Context, url string) (data []byte, contentType string, err error)
}

// Presence is a typing/recording indicator.
type Presence string

const (
	PresenceTyping    Presence = "composing"
	PresenceRecording Presence = "recording"
	PresencePaused    Presence = "paused"
)

// MaxPresenceMS is the provider's ceiling on one presence request.
const MaxPresenceMS = 300000

// MessagingAPI is the send surface.
type MessagingAPI interface {
	SendText(ctx context.Context, ref InstanceRef, in SendTextInput) (*SendResult, error)
	SendMedia(ctx context.Context, ref InstanceRef, in SendMediaInput) (*SendResult, error)
	SendMenu(ctx context.Context, ref InstanceRef, in SendMenuInput) (*SendResult, error)

	SendPresence(ctx context.Context, ref InstanceRef, chatID string, presence Presence, delayMS int) error
	MarkRead(ctx context.Context, ref InstanceRef, providerMessageIDs []string) error
	React(ctx context.Context, ref InstanceRef, chatID, providerMessageID, emoji string) error
	EditMessage(ctx context.Context, ref InstanceRef, providerMessageID, text string) (*SendResult, error)
	DeleteMessage(ctx context.Context, ref InstanceRef, providerMessageID string) error

	// DownloadMedia fetches an inbound attachment's bytes.
	DownloadMedia(ctx context.Context, ref InstanceRef, providerMessageID string) (*RemoteMedia, error)
	// ChatDetails reads one chat subject's profile — name and avatar — for a
	// person or a group.
	//
	// The ONLY profile read in the system, and it is deliberately expensive to
	// reach: it costs a provider round trip on the same instance the send budget
	// belongs to, so callers gate it behind a staleness clock and never invoke
	// it from a read path. An inbox page that fetched a profile per row would
	// fan one render into fifty provider calls.
	ChatDetails(ctx context.Context, ref InstanceRef, chatID string) (*ChatProfile, error)
	// CheckNumbers reports which of these numbers are on WhatsApp.
	//
	// Checked before opening a conversation with a number nobody has messaged
	// us from. Sending to numbers that are not registered is the loudest spam
	// signal a linked device can emit, and an operator pasting a number has no
	// way to know it is dead.
	CheckNumbers(ctx context.Context, ref InstanceRef, numbers []string) ([]NumberCheck, error)
}

// ---------------------------------------------------------------- text safety

// placeholderOpen is the provider's server-side substitution marker.
const placeholderOpen = "{{"

// SanitizeOutboundText neutralises the provider's placeholder syntax.
//
// Every send endpoint performs server-side substitution of `{{name}}`,
// `{{lead_email}}` and friends from the PROVIDER's own lead store, which is not
// our lead store. Our CRM renders its own variables before the adapter is
// called, so anything still containing `{{` at this point is either an operator
// typing it literally or an AI emitting a stray brace — and either way the
// provider would silently substitute someone else's data into a customer's
// message.
//
// A zero-width space between the braces defeats the substitution while leaving
// the text visually identical. Stripping the braces instead would silently
// alter what an operator wrote.
func SanitizeOutboundText(body string) string {
	if !strings.Contains(body, placeholderOpen) {
		return body
	}
	return strings.ReplaceAll(body, placeholderOpen, "{​{")
}

// TextTooLong reports whether a body exceeds our cap, measured in runes.
func TextTooLong(body string) bool {
	return utf8.RuneCountInString(body) > MaxTextRunes
}

// InteractiveStyle values, matching the channel-agnostic vocabulary.
const (
	InteractiveStyleButtons = "buttons"
	InteractiveStyleList    = "list"
)

// MaxOptionsFor returns WhatsApp's cap for a prompt style.
func MaxOptionsFor(style string) int {
	if style == InteractiveStyleList {
		return MaxListOptions
	}
	return MaxButtonOptions
}
