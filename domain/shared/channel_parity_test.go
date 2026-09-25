package shared

import (
	"reflect"
	"testing"
)

func TestEveryViewableChannelIsTaggable(t *testing.T) {
	for _, e := range ConversationViewableEntryTypes() {
		if !e.SupportsCRMTagging() {
			t.Errorf("%q is viewable but not taggable: its board cards would be immovable", e)
		}
	}
}

func TestEveryViewableChannelCanBeClosed(t *testing.T) {
	for _, e := range ConversationViewableEntryTypes() {
		if !e.SupportsConversationClosing() {
			t.Errorf("%q can be opened but not closed: the finish tool and workflow node would refuse it", e)
		}
	}
}

func TestEveryMessagingChannelIsInboxScopable(t *testing.T) {
	for _, e := range messagingTypesSorted() {
		if !e.SupportsInboxScope() {
			t.Errorf("%q is a messaging channel but not a valid inbox scope", e)
		}
	}
}

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

func TestContainerScopedIsSubsetOfInboxScopable(t *testing.T) {
	for _, e := range ContainerScopedInboxEntryTypes() {
		if !e.SupportsInboxScope() {
			t.Errorf("%q has a container-scoped query but is not a valid inbox scope", e)
		}
	}
}

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

func TestUnofficialWhatsAppIsFullyRegistered(t *testing.T) {
	uw := EntryTypeUnofficialWhatsApp

	checks := map[string]bool{
		"Valid (messaging pipeline)":   uw.Valid(),
		"SupportsConversationView":     uw.SupportsConversationView(),
		"SupportsCRMTagging":           uw.SupportsCRMTagging(),
		"SupportsConversationClosing":  uw.SupportsConversationClosing(),
		"SupportsInboxScope":           uw.SupportsInboxScope(),
		"SupportsContainerScopedInbox": uw.SupportsContainerScopedInbox(),
		"IsKnown":                      uw.IsKnown(),
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("unofficial_whatsapp fails %s", name)
		}
	}

	if uw == EntryTypeWhatsApp {
		t.Fatal("the two WhatsApp transports must not share an entry type")
	}
	if uw.EventChannel() != string(uw) {
		t.Errorf("EventChannel() = %q, want %q: timeline events would be labelled as another channel",
			uw.EventChannel(), uw)
	}
}

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

func messagingTypesSorted() []EntryType {
	out := make([]EntryType, 0, len(messagingEntryTypes))
	for e := range messagingEntryTypes {
		out = append(out, e)
	}
	sorted := KnownEntryTypes()
	filtered := out[:0]
	for _, e := range sorted {
		if _, ok := messagingEntryTypes[e]; ok {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func TestMessagingSetContainsEveryTextChannel(t *testing.T) {
	want := []EntryType{
		EntryTypeInstagram,
		EntryTypeTelegram,
		EntryTypeUnofficialWhatsApp,
		EntryTypeWhatsApp,
	}
	got := messagingTypesSorted()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("messaging entry types = %v, want %v", got, want)
	}
}
