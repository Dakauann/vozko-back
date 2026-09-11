package shared

import (
	"reflect"
	"testing"
)

// Valid() gates the shared messaging pipeline. Its membership is load-bearing at
// several call sites (message history, analysis filters, lead handlers), so the
// set is pinned here: a channel added to the viewable set must not silently
// become a messaging type as well.
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

// The CRM conversation view is channel-agnostic; this set is what the websocket
// handlers consult. Instagram's absence from it is what made a received DM
// impossible to open.
func TestEntryTypeSupportsConversationView(t *testing.T) {
	viewable := []EntryType{EntryTypeWhatsApp, EntryTypeTelegram, EntryTypeInstagram}
	for _, e := range viewable {
		if !e.SupportsConversationView() {
			t.Errorf("%q should be viewable in the CRM", e)
		}
	}

	notViewable := []EntryType{
		EntryTypeSupport, // handled by its own inbox, not the conversation view
		"", "messenger", "Instagram", "INSTAGRAM", " whatsapp",
	}
	for _, e := range notViewable {
		if e.SupportsConversationView() {
			t.Errorf("%q should not be viewable in the CRM", e)
		}
	}
}

// Matching is exact: a raw value off the wire is never trimmed or lowercased on
// the way in, so a near-miss must be rejected rather than silently accepted.
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

	// The exported list must mirror the predicate exactly, otherwise the error
	// message shown to an operator would name types the handler rejects.
	for _, e := range got {
		if !e.SupportsConversationView() {
			t.Errorf("%q is listed but not viewable", e)
		}
	}
	if len(got) != len(conversationViewableEntryTypes) {
		t.Errorf("listed %d types, set holds %d", len(got), len(conversationViewableEntryTypes))
	}

	// A second call must not be affected by the first (no shared backing array).
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

// Adding a channel must be a one-line domain change: the constant plus its set
// membership. This test documents that contract, if it needs editing for a new
// channel, the capability leaked back out into the callers.
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
	// The user-facing message picks the new channel up with no other edit.
	if got := FormatEntryTypes(ConversationViewableEntryTypes()); got != "'instagram', 'messenger', 'telegram', 'unofficial_whatsapp' or 'whatsapp'" {
		t.Errorf("error text did not follow the set: %q", got)
	}
	// Viewability must not imply messaging-pipeline membership.
	if messenger.Valid() {
		t.Error("viewable must not imply Valid(); the two sets are independent")
	}
}

// Analysis is TWO independent questions, not one, and neither is answered by
// Valid().
//
// A comment needs a channel with public posts under an account. A conversation
// needs a transcript, which every channel writes to conversation_messages,
// voice included, despite voice carrying no messaging pipeline. Folding the two
// into one predicate would either offer comment analysis on Telegram, which has
// no posts, or refuse conversation analysis on voice, which has the richest
// transcripts in the system.
func TestEntryTypeAnalysisSetsAreIndependent(t *testing.T) {
	// Instagram is the only channel with public comments today.
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

	// Every channel that holds a transcript can be analysed as a conversation.
	for _, e := range []EntryType{
		EntryTypeWhatsApp, EntryTypeInstagram, EntryTypeTelegram,
		EntryTypeUnofficialWhatsApp,
	} {
		if !e.SupportsConversationAnalysis() {
			t.Errorf("%q should support conversation analysis", e)
		}
	}
	// Support entries are internal tickets; the legacy engine never analysed
	// them and this port does not start.
	if EntryTypeSupport.SupportsConversationAnalysis() {
		t.Error("support should not support conversation analysis")
	}

	// Voice is the case that proves the sets are independent of Valid().
	if !EntryTypeSupport.Valid() {
		t.Fatal("precondition: support is a messaging entry type")
	}
	if EntryTypeSupport.SupportsConversationAnalysis() {
		t.Error("support is a messaging type that is deliberately never analysed")
	}
}

// Matching stays exact here too: an entry type read off a queue row or a URL is
// never normalised, so a near-miss must not open an analysis path.
func TestAnalysisSetsMatchExactly(t *testing.T) {
	for _, e := range []EntryType{"Instagram", "INSTAGRAM", " instagram", "instagram ", "", "messenger"} {
		if e.SupportsCommentAnalysis() || e.SupportsConversationAnalysis() {
			t.Errorf("%q must not match an analysis set", e)
		}
	}
}

// The exported lists mirror their predicates, so a "must be one of" message
// cannot name a channel the engine rejects.
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

// Every analysable type must be a type the system recognises at all, the same
// invariant the other sets hold against knownEntryTypes.
func TestAnalysableEntryTypesAreKnown(t *testing.T) {
	for _, e := range append(CommentAnalysableEntryTypes(), ConversationAnalysableEntryTypes()...) {
		if !e.IsKnown() {
			t.Errorf("%q is analysable but not a known entry type", e)
		}
	}
}
