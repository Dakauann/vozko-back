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
		EntryTypeVoice, // viewable, but carries no message pipeline
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
	viewable := []EntryType{EntryTypeWhatsApp, EntryTypeVoice, EntryTypeInstagram}
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
		EntryTypeVoice,
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
		{"two", []EntryType{EntryTypeVoice, EntryTypeWhatsApp}, "'voice' or 'whatsapp'"},
		{"three", []EntryType{EntryTypeInstagram, EntryTypeVoice, EntryTypeWhatsApp}, "'instagram', 'voice' or 'whatsapp'"},
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
	if got := FormatEntryTypes(ConversationViewableEntryTypes()); got != "'instagram', 'messenger', 'telegram', 'unofficial_whatsapp', 'voice' or 'whatsapp'" {
		t.Errorf("error text did not follow the set: %q", got)
	}
	// Viewability must not imply messaging-pipeline membership.
	if messenger.Valid() {
		t.Error("viewable must not imply Valid(); the two sets are independent")
	}
}
