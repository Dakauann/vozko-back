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
	ErrNoAdapterForEntryType = errors.New("conversation: no channel adapter registered for entry type")
	ErrOutboundWindowClosed  = errors.New("conversation: outbound messaging window is closed")
	ErrCapabilityUnsupported = errors.New("conversation: channel does not support this operation")
)

type EntryContext struct {
	EntryID     string
	EntryType   shared.EntryType
	WorkspaceID string

	AccountID string

	ContactID     string
	ContactRef    string
	ContactHandle string

	LastInboundAt *time.Time
}

type SendOutcome struct {
	ProviderMessageID string
}

type SendTextRequest struct {
	Body                     string
	ReplyToProviderMessageID string
	HumanInitiated           bool
}

type SendMediaRequest struct {
	Kind     string
	URL      string
	Bytes    []byte
	MIMEType string
	FileName string
	Caption  string

	ReplyToProviderMessageID string
	HumanInitiated           bool
}

type WindowClosedReason string

const (
	WindowReasonNone               WindowClosedReason = ""
	WindowReasonExpired            WindowClosedReason = "expired"
	WindowReasonNoInbound          WindowClosedReason = "no_inbound"
	WindowReasonContactBlocked     WindowClosedReason = "contact_blocked"
	WindowReasonSessionDown        WindowClosedReason = "session_down"
	WindowReasonAccountRestricted  WindowClosedReason = "account_restricted"
	WindowReasonReplyRevoked       WindowClosedReason = "reply_revoked"
	WindowReasonChannelUnavailable WindowClosedReason = "channel_unavailable"
)

type WindowState struct {
	Open      bool
	ExpiresAt *time.Time
	Reason    WindowClosedReason
}

func OpenWindow(expiresAt *time.Time) WindowState {
	return WindowState{Open: true, ExpiresAt: expiresAt, Reason: WindowReasonNone}
}

func ClosedWindow(reason WindowClosedReason) WindowState {
	return WindowState{Reason: reason}
}

func ClosedWindowUntil(reason WindowClosedReason, until *time.Time) WindowState {
	return WindowState{ExpiresAt: until, Reason: reason}
}

type ChannelAdapter interface {
	EntryType() shared.EntryType

	ResolveEntry(ctx context.Context, entryID string) (*EntryContext, error)

	WindowState(ctx context.Context, ec *EntryContext) (WindowState, error)

	SendText(ctx context.Context, ec *EntryContext, req SendTextRequest) (*SendOutcome, error)
	SendMedia(ctx context.Context, ec *EntryContext, req SendMediaRequest) (*SendOutcome, error)
}

type ReactingAdapter interface {
	SendReaction(ctx context.Context, ec *EntryContext, targetProviderMessageID, reaction string) error
	RemoveReaction(ctx context.Context, ec *EntryContext, targetProviderMessageID string) error
}

type PresenceAdapter interface {
	SendTyping(ctx context.Context, ec *EntryContext, on bool) error
	MarkSeen(ctx context.Context, ec *EntryContext, upToProviderMessageID string) error
}

type EditingAdapter interface {
	EditText(ctx context.Context, ec *EntryContext, providerMessageID, body string) error
}

type RetractingAdapter interface {
	Retract(ctx context.Context, ec *EntryContext, providerMessageID string, sentAt time.Time) error
}

type InteractiveOption struct {
	ID    string
	Title string
}

type SendInteractiveRequest struct {
	Body    string
	Header  string
	Footer  string
	Options []InteractiveOption

	Style string
}

type TypingAdapter interface {
	SendTyping(ctx context.Context, ec *EntryContext, on bool) error
}

type SeenAdapter interface {
	MarkSeen(ctx context.Context, ec *EntryContext, upToProviderMessageID string) error
}

type InteractiveAdapter interface {
	SendInteractive(ctx context.Context, ec *EntryContext, req SendInteractiveRequest) (*SendOutcome, error)

	InteractiveLimits() channel.InteractiveLimits
}

type AdapterRegistry interface {
	For(t shared.EntryType) (ChannelAdapter, error)
	Has(t shared.EntryType) bool
	EntryTypes() []shared.EntryType
}

type adapterRegistry struct {
	adapters map[shared.EntryType]ChannelAdapter
}

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
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type LiveAdapterRegistry struct {
	mu    sync.RWMutex
	inner AdapterRegistry
}

func NewLiveAdapterRegistry() *LiveAdapterRegistry {
	return &LiveAdapterRegistry{inner: NewAdapterRegistry()}
}

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
