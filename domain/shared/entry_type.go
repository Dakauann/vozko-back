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
	// EntryTypeVoice is a telephony conversation. It is written to
	// conversation_messages like any other entry type, but it is not a messaging
	// channel: it carries no inbound/outbound message pipeline, which is why it
	// is absent from the messaging set below.
	EntryTypeVoice EntryType = "voice"
)

// messagingEntryTypes are the channels the shared messaging pipeline accepts —
// the ones with inbound webhooks, an outbound send path and a message history.
//
// Voice is deliberately excluded: it has no message pipeline, and several call
// sites rely on Valid() rejecting it.
var messagingEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeSupport:   {},
	EntryTypeInstagram: {},
}

// conversationViewableEntryTypes are the entry types whose conversations the CRM
// can open, search and page through.
//
// This is a different question from Valid(): support entries are a valid
// messaging type but are not opened through the CRM conversation view, while
// voice conversations are viewable despite not being a messaging channel. Adding
// a channel (Telegram, say) means adding its constant and listing it here — no
// delivery-layer or usecase code changes.
var conversationViewableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:  {},
	EntryTypeVoice:     {},
	EntryTypeInstagram: {},
}

// crmTaggableEntryTypes are the entry types whose conversations can carry CRM
// metadata: a kanban stage and labels.
//
// A third question again, and it answers differently from both sets above —
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
// Opening a conversation is channel-agnostic — transcript, unread count and
// window state all key on (entry_id, entry_type) — so this single predicate
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
