package shared

import (
	"sort"
	"strings"
)

// EntryType identifies which channel a conversation belongs to. It is persisted
// as conversation_messages.entry_type and, paired with the entry id, keys every
// per-channel lookup in the system.
type EntryType string

const (
	EntryTypeWhatsApp  EntryType = "whatsapp"
	EntryTypeSupport   EntryType = "support"
	EntryTypeInstagram EntryType = "instagram"
	EntryTypeTelegram  EntryType = "telegram"
	// EntryTypeVoice is a telephony conversation. It is written to
	// conversation_messages like any other entry type, but it is not a messaging
	// channel: it carries no inbound/outbound message pipeline, which is why it
	// is absent from the messaging set below.
	EntryTypeVoice EntryType = "voice"
)

// messagingEntryTypes are the channels the shared messaging pipeline accepts,
// the ones with inbound webhooks, an outbound send path and a message history.
//
// Voice is deliberately excluded: it has no message pipeline, and several call
// sites rely on Valid() rejecting it.
var messagingEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeSupport:   {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
}

// conversationViewableEntryTypes are the entry types whose conversations the CRM
// can open, search and page through.
//
// This is a different question from Valid(): support entries are a valid
// messaging type but are not opened through the CRM conversation view, while
// voice conversations are viewable despite not being a messaging channel. Adding
// a channel (Telegram, say) means adding its constant and listing it here, no
// delivery-layer or usecase code changes.
var conversationViewableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeVoice:     {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
}

// crmTaggableEntryTypes are the entry types whose conversations can carry CRM
// metadata: a kanban stage and labels.
//
// A third question again, and it answers differently from both sets above,
// voice is not a messaging channel yet is staged and labelled like any other
// conversation, and support is staged despite not being opened through the CRM
// conversation view. Every channel that reaches the board belongs here: a card
// that renders but cannot be moved or labelled is worse than no card, so adding
// a channel to entry_sources.go without listing it here ships exactly that.
var crmTaggableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeVoice:     {},
	EntryTypeSupport:   {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
}

// conversationClosableEntryTypes are the entry types whose conversations can be
// moved to "finished", by an operator, by the AI's finish tool, or by a
// workflow's finish node.
//
// A fourth independent question. It used to be spelled inline as
// `entryType != "whatsapp" && entryType != "voice"` in three separate places,
// which is why Instagram conversations could be transferred and staged but never
// closed: the guards predate the channel and fail CLOSED, silently. Voice is
// listed because those guards accepted it and the status service treats it as a
// no-op; removing it here would be a behaviour change unrelated to adding a
// channel.
var conversationClosableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeVoice:     {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
}

// Valid reports whether the entry type is a messaging channel the shared
// pipeline can persist and route.
func (e EntryType) Valid() bool {
	_, ok := messagingEntryTypes[e]
	return ok
}

// SupportsCRMTagging reports whether conversations of this type can be assigned
// a stage and labels.
func (e EntryType) SupportsCRMTagging() bool {
	_, ok := crmTaggableEntryTypes[e]
	return ok
}

// CRMTaggableEntryTypes lists the taggable entry types in a stable order, so
// validation messages stay in step with the set instead of restating it.
func CRMTaggableEntryTypes() []EntryType {
	out := make([]EntryType, 0, len(crmTaggableEntryTypes))
	for t := range crmTaggableEntryTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SupportsConversationView reports whether a conversation of this type can be
// opened, searched and paged from the CRM.
//
// Opening a conversation is channel-agnostic, transcript, unread count and
// window state all key on (entry_id, entry_type), so this single predicate
// replaces the per-handler entry-type allowlists that previously had to be
// edited in lockstep whenever a channel was added.
func (e EntryType) SupportsConversationView() bool {
	_, ok := conversationViewableEntryTypes[e]
	return ok
}

// ConversationViewableEntryTypes lists the viewable entry types in a stable
// order, so error messages and API docs stay in step with the set above instead
// of restating it as a literal.
func ConversationViewableEntryTypes() []EntryType {
	out := make([]EntryType, 0, len(conversationViewableEntryTypes))
	for t := range conversationViewableEntryTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// knownEntryTypes is every entry type the system recognises at all, the union
// of the sets above.
//
// It answers the weakest question there is: "is this path variable a real entry
// type?" That is what the HTTP conversation endpoints actually need, and they
// spelled it as `!= "voice" && != "whatsapp" && != "support"`, which rejected
// Instagram with a 400 on nine endpoints. entry_type_test.go asserts this stays
// the union, so a channel added to any set above cannot be missing here.
var knownEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeSupport:   {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
	EntryTypeVoice:     {},
}

// IsKnown reports whether this is an entry type the system recognises.
func (e EntryType) IsKnown() bool {
	_, ok := knownEntryTypes[e]
	return ok
}

// KnownEntryTypes lists every entry type in a stable order.
func KnownEntryTypes() []EntryType {
	out := make([]EntryType, 0, len(knownEntryTypes))
	for t := range knownEntryTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// inboxScopableEntryTypes are the entry types the inbox can be narrowed to, the
// valid values of the websocket's `campaignType` selector and of
// SearchInboxInput.CampaignType.
//
// A fifth independent question, and it answers differently again: support IS a
// valid inbox scope despite not being opened through the conversation view, and
// voice IS one despite having no entry_sources branch (the dialer's inbox is
// scoped separately). Spelled inline it read
// `!= "voice" && != "whatsapp" && != "support"`, which rejected Instagram with a
// 400 and would have rejected Telegram the same way.
var inboxScopableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeVoice:     {},
	EntryTypeSupport:   {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
}

// SupportsInboxScope reports whether the inbox can be narrowed to this channel.
func (e EntryType) SupportsInboxScope() bool {
	_, ok := inboxScopableEntryTypes[e]
	return ok
}

// InboxScopableEntryTypes lists the scopable entry types in a stable order, so
// the "must be one of" error stays in step with the set.
func InboxScopableEntryTypes() []EntryType {
	out := make([]EntryType, 0, len(inboxScopableEntryTypes))
	for t := range inboxScopableEntryTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// containerScopedInboxEntryTypes are the entry types whose inbox can be narrowed
// to a single container, a WhatsApp campaign, or the account row for channels
// that have no campaign concept.
//
// This is narrower than SupportsInboxScope: voice and support are valid inbox
// selectors but have no container-scoped query behind them, and asking for one
// would return an empty list rather than the workspace-wide view they show
// today. infra/repositories/conversation/channel_queries.go is the registry that
// actually serves these, and entry_type_test.go pins the two in step.
var containerScopedInboxEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeInstagram: {},
	EntryTypeTelegram:  {},
}

// SupportsContainerScopedInbox reports whether the inbox can be narrowed to one
// of this channel's containers.
func (e EntryType) SupportsContainerScopedInbox() bool {
	_, ok := containerScopedInboxEntryTypes[e]
	return ok
}

// ContainerScopedInboxEntryTypes lists them in a stable order.
func ContainerScopedInboxEntryTypes() []EntryType {
	out := make([]EntryType, 0, len(containerScopedInboxEntryTypes))
	for t := range containerScopedInboxEntryTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SupportsConversationClosing reports whether a conversation of this type can be
// finished.
func (e EntryType) SupportsConversationClosing() bool {
	_, ok := conversationClosableEntryTypes[e]
	return ok
}

// ConversationClosableEntryTypes lists the closable entry types in a stable
// order, so error messages stay in step with the set instead of restating it.
func ConversationClosableEntryTypes() []EntryType {
	out := make([]EntryType, 0, len(conversationClosableEntryTypes))
	for t := range conversationClosableEntryTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// FormatEntryTypes renders entry types for a user-facing message, e.g.
// "'instagram', 'voice' or 'whatsapp'".
func FormatEntryTypes(types []EntryType) string {
	quoted := make([]string, 0, len(types))
	for _, t := range types {
		quoted = append(quoted, "'"+string(t)+"'")
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

func (e EntryType) String() string {
	return string(e)
}

// EventChannel is the channel label carried on conversation timeline events.
//
// Every entry type names its own channel, so this is the identity, but it was
// written twice as a switch with `default: "whatsapp"`, which labelled every
// Instagram and Telegram event as WhatsApp on the timeline and in any report
// grouped by channel. A channel added later inherited the same wrong default
// silently.
//
// An unknown type keeps the historical "whatsapp" fallback rather than emitting
// an empty label into stored events, since the value is persisted.
func (e EntryType) EventChannel() string {
	if e.IsKnown() {
		return string(e)
	}
	return string(EntryTypeWhatsApp)
}
