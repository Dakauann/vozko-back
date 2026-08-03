package conversation

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"vozko/domain/channel"
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

// EditingAdapter is implemented by channels where an already-sent message can be
// corrected.
//
// Only Telegram can do this today. It is an optional capability rather than a
// method on every adapter precisely so the UI can hide the action for channels
// that cannot honour it: offering "edit" on WhatsApp and then failing is worse
// than not offering it.
type EditingAdapter interface {
	EditText(ctx context.Context, ec *EntryContext, providerMessageID, body string) error
}

// RetractingAdapter is implemented by channels where a sent message can be
// unsent.
//
// sentAt lets the adapter enforce the provider's own time bound (Telegram: 48
// hours) and explain the refusal, instead of surfacing an opaque provider error
// to an operator who is trying to undo a mistake.
type RetractingAdapter interface {
	Retract(ctx context.Context, ec *EntryContext, providerMessageID string, sentAt time.Time) error
}

// InteractiveOption is one choice offered to the contact.
//
// ID is the contract and Title is the display string. Everything downstream,
// the workflow node's branching, the stored message text, keys off ID, because
// a title is a label an author edits freely and a payload is an identifier a
// running conversation depends on.
type InteractiveOption struct {
	ID    string
	Title string
}

// SendInteractiveRequest is a channel-agnostic "pick one" prompt.
//
// Header and Footer are best-effort: WhatsApp renders both, Instagram and
// Telegram have no such slots and fold them into the body rather than dropping
// the author's words.
type SendInteractiveRequest struct {
	Body    string
	Header  string
	Footer  string
	Options []InteractiveOption

	// Style is buttons | list. Channels with one native mechanism ignore it;
	// it exists because WhatsApp picks a different message type from it.
	Style string
}

// InteractiveAdapter is implemented by channels that can ask the contact to
// pick one option and report which was picked.
//
// Optional, like ReactingAdapter and EditingAdapter, and for the same reason:
// the workflow editor and the CRM must be able to ask "can this channel do it?"
// before offering the affordance. A channel that cannot present choices must
// not silently send a wall of text listing them.
type InteractiveAdapter interface {
	// SendInteractive delivers the prompt. The adapter is responsible for
	// applying its own channel's limits, the caller passes the author's full
	// option list and the adapter renders what it can.
	SendInteractive(ctx context.Context, ec *EntryContext, req SendInteractiveRequest) (*SendOutcome, error)

	// InteractiveLimits reports what this channel will actually render, so a
	// caller can warn before sending rather than explain afterwards.
	InteractiveLimits() channel.InteractiveLimits
}

// AdapterRegistry resolves adapters by entry type.
type AdapterRegistry interface {
	For(t shared.EntryType) (ChannelAdapter, error)
	Has(t shared.EntryType) bool
	// EntryTypes lists every registered channel, sorted, so callers that must
	// describe ALL channels, the workflow editor asking which ones render an
	// interactive prompt, can enumerate instead of hardcoding a list that goes
	// stale the next time a channel is added.
	EntryTypes() []shared.EntryType
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

func (r *adapterRegistry) EntryTypes() []shared.EntryType {
	out := make([]shared.EntryType, 0, len(r.adapters))
	for t := range r.adapters {
		out = append(out, t)
	}
	// Sorted so the editor's channel list has a stable order between requests
	// rather than Go's randomized map iteration.
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// LiveAdapterRegistry is an AdapterRegistry whose contents can be replaced after
// it has been handed out.
//
// Channels register their adapters one at a time as the container initializes,
// so anything wired before the last channel would otherwise hold a snapshot
// missing it. That failure is silent and badly misleading: a missing adapter
// reads downstream as "this channel cannot send", so a workflow simply skips
// every send node on that channel and reports the run as completed.
//
// Handing consumers this instead means registration order stops mattering.
type LiveAdapterRegistry struct {
	mu    sync.RWMutex
	inner AdapterRegistry
}

func NewLiveAdapterRegistry() *LiveAdapterRegistry {
	return &LiveAdapterRegistry{inner: NewAdapterRegistry()}
}

// Replace swaps in a registry built from the adapters known so far.
func (r *LiveAdapterRegistry) Replace(adapters ...ChannelAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inner = NewAdapterRegistry(adapters...)
}

func (r *LiveAdapterRegistry) For(t shared.EntryType) (ChannelAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.For(t)
}

func (r *LiveAdapterRegistry) Has(t shared.EntryType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.Has(t)
}

func (r *LiveAdapterRegistry) EntryTypes() []shared.EntryType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inner.EntryTypes()
}
