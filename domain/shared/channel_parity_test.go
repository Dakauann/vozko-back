package shared

import (
	"reflect"
	"testing"
)

// Every guard in this file exists because a real feature was silently unavailable
// on a real channel for months.
//
// The pattern was always the same: a call site asked `entryType == "whatsapp"`
// instead of asking what the channel can DO, and the `default` branch returned
// "not supported" with no error and no log line. Instagram shipped that way — its
// conversations could be transferred and staged but never closed, never analysed,
// never exported, and never counted in any dashboard.
//
// These tests pin the predicate sets together so the NEXT channel cannot inherit
// the same holes by omission.

// A channel that reaches the kanban board must be able to carry a stage and a
// label. A card that renders but cannot be moved is worse than no card.
func TestEveryViewableChannelIsTaggable(t *testing.T) {
	for _, e := range ConversationViewableEntryTypes() {
		if !e.SupportsCRMTagging() {
			t.Errorf("%q is viewable but not taggable: its board cards would be immovable", e)
		}
	}
}

// A conversation an operator can open is one they will eventually want to close.
// The AI finish tool, the workflow finish node and the manual close all consult
// SupportsConversationClosing.
func TestEveryViewableChannelCanBeClosed(t *testing.T) {
	for _, e := range ConversationViewableEntryTypes() {
		if !e.SupportsConversationClosing() {
			t.Errorf("%q can be opened but not closed: the finish tool and workflow node would refuse it", e)
		}
	}
}

// Every messaging channel must be a valid inbox scope, or an operator cannot
// narrow the inbox to it and the websocket rejects the connection with a 400.
func TestEveryMessagingChannelIsInboxScopable(t *testing.T) {
	for _, e := range messagingTypesSorted() {
		if !e.SupportsInboxScope() {
			t.Errorf("%q is a messaging channel but not a valid inbox scope", e)
		}
	}
}

// IsKnown is the weakest predicate — "is this a real entry type?" — and the HTTP
// conversation endpoints depend on it. It must be the union of every other set,
// or an endpoint rejects a channel the rest of the system accepts.
func TestIsKnownIsTheUnionOfEverySet(t *testing.T) {
	union := map[EntryType]struct{}{}
	for _, set := range []map[EntryType]struct{}{
		messagingEntryTypes,
		conversationViewableEntryTypes,
		crmTaggableEntryTypes,
		conversationClosableEntryTypes,
		inboxScopableEntryTypes,
		containerScopedInboxEntryTypes,
	} {
		for e := range set {
			union[e] = struct{}{}
		}
	}

	for e := range union {
		if !e.IsKnown() {
			t.Errorf("%q is in a predicate set but not IsKnown: nine HTTP endpoints would 400 on it", e)
		}
	}
	for e := range knownEntryTypes {
		if _, ok := union[e]; !ok {
			t.Errorf("%q is IsKnown but in no predicate set: it can reach endpoints nothing else supports", e)
		}
	}
}

// A container-scoped inbox is necessarily a valid inbox scope. The reverse does
// not hold — voice and support are selectable but have no container query — which
// is exactly why they are separate sets.
func TestContainerScopedIsSubsetOfInboxScopable(t *testing.T) {
	for _, e := range ContainerScopedInboxEntryTypes() {
		if !e.SupportsInboxScope() {
			t.Errorf("%q has a container-scoped query but is not a valid inbox scope", e)
		}
	}
}

// Telegram is registered everywhere a messaging channel belongs. This is the
// concrete assertion the abstract ones above cannot make: a channel could satisfy
// every invariant by being absent from all of them.
func TestTelegramIsFullyRegistered(t *testing.T) {
	tg := EntryTypeTelegram

	checks := map[string]bool{
		"Valid (messaging pipeline)":   tg.Valid(),
		"SupportsConversationView":     tg.SupportsConversationView(),
		"SupportsCRMTagging":           tg.SupportsCRMTagging(),
		"SupportsConversationClosing":  tg.SupportsConversationClosing(),
		"SupportsInboxScope":           tg.SupportsInboxScope(),
		"SupportsContainerScopedInbox": tg.SupportsContainerScopedInbox(),
		"IsKnown":                      tg.IsKnown(),
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("telegram fails %s", name)
		}
	}
}

// Instagram must keep everything Telegram gained. The parity work was done once,
// generically; a later change that re-narrows a predicate would silently take
// these back from Instagram too.
func TestInstagramKeepsFullParity(t *testing.T) {
	ig := EntryTypeInstagram

	checks := map[string]bool{
		"Valid (messaging pipeline)":   ig.Valid(),
		"SupportsConversationView":     ig.SupportsConversationView(),
		"SupportsCRMTagging":           ig.SupportsCRMTagging(),
		"SupportsConversationClosing":  ig.SupportsConversationClosing(),
		"SupportsInboxScope":           ig.SupportsInboxScope(),
		"SupportsContainerScopedInbox": ig.SupportsContainerScopedInbox(),
		"IsKnown":                      ig.IsKnown(),
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("instagram fails %s", name)
		}
	}
}

// WhatsApp is the revenue path. Nothing in the parity refactor may narrow it.
func TestWhatsAppKeepsEveryCapability(t *testing.T) {
	wa := EntryTypeWhatsApp

	checks := map[string]bool{
		"Valid":                        wa.Valid(),
		"SupportsConversationView":     wa.SupportsConversationView(),
		"SupportsCRMTagging":           wa.SupportsCRMTagging(),
		"SupportsConversationClosing":  wa.SupportsConversationClosing(),
		"SupportsInboxScope":           wa.SupportsInboxScope(),
		"SupportsContainerScopedInbox": wa.SupportsContainerScopedInbox(),
		"IsKnown":                      wa.IsKnown(),
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("whatsapp lost %s", name)
		}
	}
}

// An unregistered channel must satisfy nothing. This is the guard that proves the
// predicates are real membership tests rather than accidentally-true defaults.
func TestUnregisteredChannelSatisfiesNothing(t *testing.T) {
	const messenger EntryType = "messenger"

	for name, ok := range map[string]bool{
		"Valid":                        messenger.Valid(),
		"SupportsConversationView":     messenger.SupportsConversationView(),
		"SupportsCRMTagging":           messenger.SupportsCRMTagging(),
		"SupportsConversationClosing":  messenger.SupportsConversationClosing(),
		"SupportsInboxScope":           messenger.SupportsInboxScope(),
		"SupportsContainerScopedInbox": messenger.SupportsContainerScopedInbox(),
		"IsKnown":                      messenger.IsKnown(),
	} {
		if ok {
			t.Errorf("an unregistered channel must not satisfy %s", name)
		}
	}
}

// The exported lists must mirror their predicates exactly: they are rendered into
// user-facing "must be one of …" messages, and a list naming a type the handler
// rejects sends an operator chasing a value that cannot work.
func TestExportedListsMirrorTheirPredicates(t *testing.T) {
	cases := []struct {
		name  string
		list  []EntryType
		check func(EntryType) bool
	}{
		{"ConversationViewableEntryTypes", ConversationViewableEntryTypes(), EntryType.SupportsConversationView},
		{"CRMTaggableEntryTypes", CRMTaggableEntryTypes(), EntryType.SupportsCRMTagging},
		{"ConversationClosableEntryTypes", ConversationClosableEntryTypes(), EntryType.SupportsConversationClosing},
		{"InboxScopableEntryTypes", InboxScopableEntryTypes(), EntryType.SupportsInboxScope},
		{"ContainerScopedInboxEntryTypes", ContainerScopedInboxEntryTypes(), EntryType.SupportsContainerScopedInbox},
		{"KnownEntryTypes", KnownEntryTypes(), EntryType.IsKnown},
	}

	for _, c := range cases {
		if len(c.list) == 0 {
			t.Errorf("%s is empty", c.name)
		}
		for _, e := range c.list {
			if !c.check(e) {
				t.Errorf("%s lists %q but the predicate rejects it", c.name, e)
			}
		}
		// Sorted, so the rendered message is stable rather than map-order random.
		sorted := append([]EntryType(nil), c.list...)
		for i := 1; i < len(sorted); i++ {
			if sorted[i-1] > sorted[i] {
				t.Errorf("%s is not sorted: %v", c.name, c.list)
				break
			}
		}
	}
}

func TestFormatEntryTypesReadsAsProse(t *testing.T) {
	cases := map[string]struct {
		in   []EntryType
		want string
	}{
		"none":  {nil, ""},
		"one":   {[]EntryType{EntryTypeTelegram}, "'telegram'"},
		"two":   {[]EntryType{EntryTypeTelegram, EntryTypeWhatsApp}, "'telegram' or 'whatsapp'"},
		"three": {[]EntryType{EntryTypeInstagram, EntryTypeTelegram, EntryTypeWhatsApp}, "'instagram', 'telegram' or 'whatsapp'"},
	}
	for name, c := range cases {
		if got := FormatEntryTypes(c.in); got != c.want {
			t.Errorf("%s: FormatEntryTypes = %q, want %q", name, got, c.want)
		}
	}
}

// messagingTypesSorted lists the messaging set deterministically.
func messagingTypesSorted() []EntryType {
	out := make([]EntryType, 0, len(messagingEntryTypes))
	for e := range messagingEntryTypes {
		out = append(out, e)
	}
	// Reuse the exported sort ordering so the two never disagree.
	sorted := KnownEntryTypes()
	filtered := out[:0]
	for _, e := range sorted {
		if _, ok := messagingEntryTypes[e]; ok {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// A guard against silently dropping a channel from the messaging set, which would
// make Valid() reject its messages on write.
func TestMessagingSetContainsEveryTextChannel(t *testing.T) {
	want := []EntryType{EntryTypeInstagram, EntryTypeSupport, EntryTypeTelegram, EntryTypeWhatsApp}
	got := messagingTypesSorted()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("messaging entry types = %v, want %v", got, want)
	}
}
