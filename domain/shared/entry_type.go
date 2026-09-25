package shared

import (
	"sort"
	"strings"
)

type EntryType string

const (
	EntryTypeWhatsApp           EntryType = "whatsapp"
	EntryTypeInstagram          EntryType = "instagram"
	EntryTypeTelegram           EntryType = "telegram"
	EntryTypeUnofficialWhatsApp EntryType = "unofficial_whatsapp"
)

var messagingEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

var conversationViewableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

var crmTaggableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

var conversationClosableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

func (e EntryType) Valid() bool {
	_, ok := messagingEntryTypes[e]
	return ok
}

func (e EntryType) SupportsCRMTagging() bool {
	_, ok := crmTaggableEntryTypes[e]
	return ok
}

func CRMTaggableEntryTypes() []EntryType {
	return sortedEntryTypes(crmTaggableEntryTypes)
}

func (e EntryType) SupportsConversationView() bool {
	_, ok := conversationViewableEntryTypes[e]
	return ok
}

func ConversationViewableEntryTypes() []EntryType {
	return sortedEntryTypes(conversationViewableEntryTypes)
}

var knownEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

func (e EntryType) IsKnown() bool {
	_, ok := knownEntryTypes[e]
	return ok
}

func KnownEntryTypes() []EntryType {
	return sortedEntryTypes(knownEntryTypes)
}

var inboxScopableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

func (e EntryType) SupportsInboxScope() bool {
	_, ok := inboxScopableEntryTypes[e]
	return ok
}

func InboxScopableEntryTypes() []EntryType {
	return sortedEntryTypes(inboxScopableEntryTypes)
}

var containerScopedInboxEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

func (e EntryType) SupportsContainerScopedInbox() bool {
	_, ok := containerScopedInboxEntryTypes[e]
	return ok
}

func ContainerScopedInboxEntryTypes() []EntryType {
	return sortedEntryTypes(containerScopedInboxEntryTypes)
}

func (e EntryType) SupportsConversationClosing() bool {
	_, ok := conversationClosableEntryTypes[e]
	return ok
}

func ConversationClosableEntryTypes() []EntryType {
	return sortedEntryTypes(conversationClosableEntryTypes)
}

var templateEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp: {},
}

func (e EntryType) SupportsTemplates() bool {
	_, ok := templateEntryTypes[e]
	return ok
}

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

func (e EntryType) EventChannel() string {
	if e.IsKnown() {
		return string(e)
	}
	return string(EntryTypeWhatsApp)
}

var commentAnalysableEntryTypes = map[EntryType]struct{}{
	EntryTypeInstagram: {},
}

var conversationAnalysableEntryTypes = map[EntryType]struct{}{
	EntryTypeWhatsApp:           {},
	EntryTypeInstagram:          {},
	EntryTypeTelegram:           {},
	EntryTypeUnofficialWhatsApp: {},
}

func (e EntryType) SupportsCommentAnalysis() bool {
	_, ok := commentAnalysableEntryTypes[e]
	return ok
}

func (e EntryType) SupportsConversationAnalysis() bool {
	_, ok := conversationAnalysableEntryTypes[e]
	return ok
}

func (e EntryType) SupportsAnalysis() bool {
	return e.SupportsCommentAnalysis() || e.SupportsConversationAnalysis()
}

func CommentAnalysableEntryTypes() []EntryType {
	return sortedEntryTypes(commentAnalysableEntryTypes)
}

func ConversationAnalysableEntryTypes() []EntryType {
	return sortedEntryTypes(conversationAnalysableEntryTypes)
}

func sortedEntryTypes(set map[EntryType]struct{}) []EntryType {
	out := make([]EntryType, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
