package conversation

import (
	"context"
	"errors"
	"time"

	"vozko/domain/shared"
)

var (
	// ErrNoAdapterForEntryType means no channel adapter is registered for the
	// entry type. Callers should treat it as a configuration error, not a
	// transient failure.
	ErrNoAdapterForEntryType = errors.New("conversation: no channel adapter registered for entry type")
	// ErrOutboundWindowClosed means the provider will not accept a message
	// right now. On Instagram this is normal: the 24h window closes and only a
	// new inbound message reopens it.
	ErrOutboundWindowClosed = errors.New("conversation: outbound messaging window is closed")
	// ErrCapabilityUnsupported means the channel cannot do what was asked.
	ErrCapabilityUnsupported = errors.New("conversation: channel does not support this operation")
)

// EntryContext is everything needed to send on any channel, resolved from an
// entry id. It replaces the per-channel tuple that
// MessageSenderService.getEntryInfo returns today via a `switch entryType`.
type EntryContext struct {
	EntryID     string
	EntryType   shared.EntryType
	WorkspaceID string

	// AccountID is the channel account that owns the conversation: a WhatsApp
	// business phone id, or an Instagram account id. It is what guarantees a
	// reply leaves from the same account the message arrived on.
	AccountID string

	// ContactID is our internal contact primary key.
	ContactID string
	// ContactRef is the provider-facing address: an E.164 number on WhatsApp,
	// an IGSID on Instagram.
	ContactRef string
	// ContactHandle is the human-readable handle (@username) when the channel
	// has one.
	ContactHandle string

	// LastInboundAt anchors the outbound window.
	LastInboundAt *time.Time
}

// SendOutcome is the provider's acknowledgement of a send.
type SendOutcome struct {
	// ProviderMessageID is the id the provider assigned. Store it so the later
	// echo/status webhook can be reconciled against the row we just wrote.
	ProviderMessageID string
}

// SendTextRequest is a channel-agnostic text send.
type SendTextRequest struct {
	Body string
	// ReplyToProviderMessageID quotes an earlier message when the channel
	// supports it.
	ReplyToProviderMessageID string
}

// SendMediaRequest is a channel-agnostic media send. Exactly one of URL or
// Bytes is expected; adapters that need a publicly fetchable URL will reject
// Bytes with ErrCapabilityUnsupported.
type SendMediaRequest struct {
	Kind     string // image | video | audio | document
	URL      string
	Bytes    []byte
	MIMEType string
	FileName string
	Caption  string

	ReplyToProviderMessageID string
}

// ChannelAdapter is the send-side port for one channel.
//
// Instagram is the first implementation. WhatsApp keeps its current code path
// until its adapter is added, at which point MessageSenderService loses its
// entry-type switch entirely.
type ChannelAdapter interface {
	EntryType() shared.EntryType

	// ResolveEntry loads everything needed to send to this entry.
	ResolveEntry(ctx context.Context, entryID string) (*EntryContext, error)

	// WindowState reports whether an outbound message is currently allowed and,
	// when known, when the window closes. A channel without a window returns
	// (true, nil, nil).
	WindowState(ctx context.Context, ec *EntryContext) (open bool, expiresAt *time.Time, err error)

	SendText(ctx context.Context, ec *EntryContext, req SendTextRequest) (*SendOutcome, error)
	SendMedia(ctx context.Context, ec *EntryContext, req SendMediaRequest) (*SendOutcome, error)
}

// ReactingAdapter is implemented by channels that support message reactions.
// Discovered by type assertion, matching the codebase's existing pattern for
// optional provider capabilities, so message-only fakes need not implement it.
type ReactingAdapter interface {
	SendReaction(ctx context.Context, ec *EntryContext, targetProviderMessageID, reaction string) error
	RemoveReaction(ctx context.Context, ec *EntryContext, targetProviderMessageID string) error
}

// PresenceAdapter is implemented by channels with typing indicators and read
// receipts.
type PresenceAdapter interface {
	SendTyping(ctx context.Context, ec *EntryContext, on bool) error
	MarkSeen(ctx context.Context, ec *EntryContext, upToProviderMessageID string) error
}

// AdapterRegistry resolves adapters by entry type.
type AdapterRegistry interface {
	For(t shared.EntryType) (ChannelAdapter, error)
	Has(t shared.EntryType) bool
}

type adapterRegistry struct {
	adapters map[shared.EntryType]ChannelAdapter
}

// NewAdapterRegistry builds an immutable registry from the given adapters.
func NewAdapterRegistry(adapters ...ChannelAdapter) AdapterRegistry {
	m := make(map[shared.EntryType]ChannelAdapter, len(adapters))
	for _, a := range adapters {
		if a == nil {
			continue
		}
		m[a.EntryType()] = a
	}
	return &adapterRegistry{adapters: m}
}

func (r *adapterRegistry) For(t shared.EntryType) (ChannelAdapter, error) {
	if a, ok := r.adapters[t]; ok {
		return a, nil
	}
	return nil, ErrNoAdapterForEntryType
}

func (r *adapterRegistry) Has(t shared.EntryType) bool {
	_, ok := r.adapters[t]
	return ok
}
