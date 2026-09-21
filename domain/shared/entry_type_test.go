package shared

import (
	"reflect"
	"testing"
)

func TestEntryTypeValid(t *testing.T) {
	valid := []EntryType{EntryTypeWhatsApp, EntryTypeSupport, EntryTypeInstagram}
	for _, e := range valid {
		if !e.Valid() {
			t.Errorf("%q should be a valid messaging entry type", e)
		}
	}

	invalid := []EntryType{
		"", "WHATSAPP", "messenger", "whatsapp ", "instagram\n",
	}
	for _, e := range invalid {
		if e.Valid() {
			t.Errorf("%q should not be a valid messaging entry type", e)
		}
	}
}

func TestEntryTypeSupportsConversationView(t *testing.T) {
	viewable := []EntryType{EntryTypeWhatsApp, EntryTypeTelegram, EntryTypeInstagram}
	for _, e := range viewable {
		if !e.SupportsConversationView() {
			t.Errorf("%q should be viewable in the CRM", e)
		}
	}

	notViewable := []EntryType{
		EntryTypeSupport,
		"", "messenger", "Instagram", "INSTAGRAM", " whatsapp",
	}
	for _, e := range notViewable {
		if e.SupportsConversationView() {
			t.Errorf("%q should not be viewable in the CRM", e)
		}
	}
}

func TestEntryTypeMatchingIsExact(t *testing.T) {
	for _, e := range []EntryType{"Whatsapp", "WHATSAPP", "whats app", "wha", "whatsappx"} {
		if e.Valid() || e.SupportsConversationView() {
			t.Errorf("%q must not match any entry-type set", e)
		}
	}
}

func TestConversationViewableEntryTypesIsStableAndComplete(t *testing.T) {
	got := ConversationViewableEntryTypes()
	want := []EntryType{
		EntryTypeInstagram,
		EntryTypeTelegram,
		EntryTypeUnofficialWhatsApp,
		EntryTypeWhatsApp,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConversationViewableEntryTypes() = %v, want %v (sorted)", got, want)
	}

	for _, e := range got {
		if !e.SupportsConversationView() {
			t.Errorf("%q is listed but not viewable", e)
		}
	}
	if len(got) != len(conversationViewableEntryTypes) {
		t.Errorf("listed %d types, set holds %d", len(got), len(conversationViewableEntryTypes))
	}

	got[0] = "mutated"
	if ConversationViewableEntryTypes()[0] == "mutated" {
		t.Error("callers can mutate the internal set through the returned slice")
	}
}

func TestFormatEntryTypes(t *testing.T) {
	cases := []struct {
		name string
		in   []EntryType
		want string
	}{
		{"empty", nil, ""},
		{"one", []EntryType{EntryTypeWhatsApp}, "'whatsapp'"},
		{"two", []EntryType{EntryTypeTelegram, EntryTypeWhatsApp}, "'telegram' or 'whatsapp'"},
		{"three", []EntryType{EntryTypeInstagram, EntryTypeTelegram, EntryTypeWhatsApp}, "'instagram', 'telegram' or 'whatsapp'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatEntryTypes(tc.in); got != tc.want {
				t.Errorf("FormatEntryTypes(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAddingAChannelTouchesOnlyTheDomainSets(t *testing.T) {
	const messenger EntryType = "messenger"

	if messenger.SupportsConversationView() {
		t.Fatal("precondition: messenger is not registered yet")
	}
	conversationViewableEntryTypes[messenger] = struct{}{}
	t.Cleanup(func() { delete(conversationViewableEntryTypes, messenger) })

	if !messenger.SupportsConversationView() {
		t.Error("registering the type should make it viewable")
	}
	if got := FormatEntryTypes(ConversationViewableEntryTypes()); got != "'instagram', 'messenger', 'telegram', 'unofficial_whatsapp' or 'whatsapp'" {
		t.Errorf("error text did not follow the set: %q", got)
	}
	if messenger.Valid() {
		t.Error("viewable must not imply Valid(); the two sets are independent")
	}
}

func TestEntryTypeAnalysisSetsAreIndependent(t *testing.T) {
	if !EntryTypeInstagram.SupportsCommentAnalysis() {
		t.Error("instagram should support comment analysis")
	}
	for _, e := range []EntryType{
		EntryTypeWhatsApp, EntryTypeTelegram, EntryTypeUnofficialWhatsApp,
		EntryTypeSupport,
	} {
		if e.SupportsCommentAnalysis() {
			t.Errorf("%q has no public comments and must not support comment analysis", e)
		}
	}

	for _, e := range []EntryType{
		EntryTypeWhatsApp, EntryTypeInstagram, EntryTypeTelegram,
		EntryTypeUnofficialWhatsApp,
	} {
		if !e.SupportsConversationAnalysis() {
			t.Errorf("%q should support conversation analysis", e)
		}
	}
	if EntryTypeSupport.SupportsConversationAnalysis() {
		t.Error("support should not support conversation analysis")
	}

	if !EntryTypeSupport.Valid() {
		t.Fatal("precondition: support is a messaging entry type")
	}
	if EntryTypeSupport.SupportsConversationAnalysis() {
		t.Error("support is a messaging type that is deliberately never analysed")
	}
}

func TestAnalysisSetsMatchExactly(t *testing.T) {
	for _, e := range []EntryType{"Instagram", "INSTAGRAM", " instagram", "instagram ", "", "messenger"} {
		if e.SupportsCommentAnalysis() || e.SupportsConversationAnalysis() {
			t.Errorf("%q must not match an analysis set", e)
		}
	}
}

func TestAnalysableEntryTypeListsMirrorPredicates(t *testing.T) {
	comment := CommentAnalysableEntryTypes()
	if !reflect.DeepEqual(comment, []EntryType{EntryTypeInstagram}) {
		t.Errorf("CommentAnalysableEntryTypes() = %v", comment)
	}
	for _, e := range comment {
		if !e.SupportsCommentAnalysis() {
			t.Errorf("%q listed but not comment-analysable", e)
		}
	}

	conversation := ConversationAnalysableEntryTypes()
	want := []EntryType{
		EntryTypeInstagram,
		EntryTypeTelegram,
		EntryTypeUnofficialWhatsApp,
		EntryTypeWhatsApp,
	}
	if !reflect.DeepEqual(conversation, want) {
		t.Errorf("ConversationAnalysableEntryTypes() = %v, want %v (sorted)", conversation, want)
	}
	for _, e := range conversation {
		if !e.SupportsConversationAnalysis() {
			t.Errorf("%q listed but not conversation-analysable", e)
		}
	}
}

func TestAnalysableEntryTypesAreKnown(t *testing.T) {
	for _, e := range append(CommentAnalysableEntryTypes(), ConversationAnalysableEntryTypes()...) {
		if !e.IsKnown() {
			t.Errorf("%q is analysable but not a known entry type", e)
		}
	}
}
