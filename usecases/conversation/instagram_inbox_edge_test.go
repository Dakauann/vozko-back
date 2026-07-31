package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// These tests exist to protect the WhatsApp-only tenant. The Instagram channel
// was added by widening shared code paths — the inbox list, the conversation-open
// gate, the window check and the sender hydration — so every one of them is
// exercised here with WhatsApp data to prove the widening changed nothing for it.

// ---------------------------------------------------------------- isolation

// A workspace that has never connected Instagram must behave exactly as before:
// the hydration pass is a no-op and cannot mutate, reorder or drop rows.
func TestHydrateInstagramSenders_WhatsAppOnlyWorkspaceUntouched(t *testing.T) {
	fake := igContactsFixture()
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(fake)

	before := []conversation.InboxEntry{
		{EntryID: "wa-1", EntryType: "whatsapp", LeadID: "lead-1", LeadName: "Ana", LeadNumber: "+5511111111111", LeadPicture: "p1", LastMessageSender: "Ana"},
		{EntryID: "wa-2", EntryType: "whatsapp", LeadID: "lead-2", LeadName: "Bruno", LeadNumber: "+5511222222222"},
		{EntryID: "sup-1", EntryType: "support", LeadID: "lead-3", LeadName: "Carla"},
		{EntryID: "voice-1", EntryType: "voice", LeadID: "lead-4", LeadName: "Diego"},
	}
	entries := append([]conversation.InboxEntry(nil), before...)

	svc.hydrateInstagramSenders(entries)

	if fake.calls != 0 {
		t.Errorf("contact lookup ran %d times for a WhatsApp-only page; want 0 (no wasted query)", fake.calls)
	}
	if len(entries) != len(before) {
		t.Fatalf("entry count changed: %d -> %d", len(before), len(entries))
	}
	for i := range before {
		// InboxEntry holds maps, so compare the fields hydration could touch.
		if entries[i].LeadName != before[i].LeadName ||
			entries[i].LeadNumber != before[i].LeadNumber ||
			entries[i].LeadPicture != before[i].LeadPicture ||
			entries[i].LastMessageSender != before[i].LastMessageSender ||
			entries[i].LastMessageSenderAvatar != before[i].LastMessageSenderAvatar ||
			entries[i].EntryID != before[i].EntryID ||
			entries[i].EntryType != before[i].EntryType {
			t.Errorf("entry %d mutated:\n before %+v\n after  %+v", i, before[i], entries[i])
		}
	}
}

// A LeadID that collides with an Instagram contact id must NOT be hydrated when
// the row is a WhatsApp row: the entry type is the discriminator, not the id.
func TestHydrateInstagramSenders_EntryTypeIsTheDiscriminator(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{
		{EntryID: "wa-1", EntryType: "whatsapp", LeadID: "contact-1", LeadName: "Real Lead", LeadNumber: "+5511999999999"},
	}
	svc.hydrateInstagramSenders(entries)

	if entries[0].LeadName != "Real Lead" || entries[0].LeadNumber != "+5511999999999" {
		t.Errorf("a WhatsApp row was hydrated from Instagram data: %+v", entries[0])
	}
}

// Mixed pages are the real production shape once a tenant connects Instagram.
func TestHydrateInstagramSenders_MixedChannelPage(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{
		{EntryID: "wa-1", EntryType: "whatsapp", LeadID: "lead-1", LeadName: "Ana", LeadNumber: "+5511111111111"},
		{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1"},
		{EntryID: "sup-1", EntryType: "support", LeadID: "lead-3", LeadName: "Carla"},
		{EntryID: "ig-2", EntryType: "instagram", LeadID: "contact-2"},
	}
	svc.hydrateInstagramSenders(entries)

	if entries[0].LeadName != "Ana" || entries[2].LeadName != "Carla" {
		t.Error("non-Instagram rows must be preserved in a mixed page")
	}
	if entries[1].LeadName != "Maria Silva" || entries[3].LeadName != "@joao_p" {
		t.Errorf("Instagram rows not hydrated in a mixed page: %q, %q", entries[1].LeadName, entries[3].LeadName)
	}
}

// ---------------------------------------------------------------- edge data

func TestHydrateInstagramSenders_EdgeCaseInputs(t *testing.T) {
	// Empty and nil pages must be safe.
	(&HistoryProviderService{}).hydrateInstagramSenders(nil)
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())
	svc.hydrateInstagramSenders([]conversation.InboxEntry{})

	// A row with no contact id cannot be resolved and must be skipped, not queried.
	fake := igContactsFixture()
	svc2 := &HistoryProviderService{}
	svc2.SetInstagramContacts(fake)
	entries := []conversation.InboxEntry{{EntryID: "ig-1", EntryType: "instagram", LeadID: ""}}
	svc2.hydrateInstagramSenders(entries)
	if fake.calls != 0 {
		t.Errorf("lookup ran for a row with no contact id (%d calls)", fake.calls)
	}

	// A contact id the repository does not know about leaves the row as-is
	// rather than blanking or panicking.
	entries = []conversation.InboxEntry{{EntryID: "ig-1", EntryType: "instagram", LeadID: "ghost", LeadName: "prior"}}
	svc2.hydrateInstagramSenders(entries)
	if entries[0].LeadName != "prior" {
		t.Errorf("unknown contact should leave the row untouched, got %q", entries[0].LeadName)
	}
}

// An operator's own message must keep the operator as the sender label: only an
// unresolved label falls back to the contact.
func TestHydrateInstagramSenders_PreservesExistingSenderLabel(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{{
		EntryID:                 "ig-1",
		EntryType:               "instagram",
		LeadID:                  "contact-1",
		LastMessageSender:       "operator-jose",
		LastMessageSenderAvatar: "https://cdn/jose.jpg",
	}}
	svc.hydrateInstagramSenders(entries)

	if entries[0].LastMessageSender != "operator-jose" {
		t.Errorf("operator sender label overwritten: %q", entries[0].LastMessageSender)
	}
	if entries[0].LastMessageSenderAvatar != "https://cdn/jose.jpg" {
		t.Errorf("operator avatar overwritten: %q", entries[0].LastMessageSenderAvatar)
	}
	// The contact identity still lands on the row itself.
	if entries[0].LeadName != "Maria Silva" {
		t.Errorf("contact identity missing: %q", entries[0].LeadName)
	}
}

func TestInstagramDisplayNames(t *testing.T) {
	cases := []struct {
		name       string
		in         InstagramContactDisplay
		wantName   string
		wantHandle string
	}{
		{"full profile", InstagramContactDisplay{Name: "Maria Silva", Handle: "mariasilva"}, "Maria Silva", "@mariasilva"},
		{"handle already prefixed", InstagramContactDisplay{Handle: "@already"}, "@already", "@already"},
		{"handle only", InstagramContactDisplay{Handle: "solo"}, "@solo", "@solo"},
		{"name only", InstagramContactDisplay{Name: "Só Nome"}, "Só Nome", ""},
		{"nothing known", InstagramContactDisplay{}, "Instagram", ""},
		{"whitespace only", InstagramContactDisplay{Name: "   ", Handle: "  "}, "Instagram", ""},
		{"unicode + emoji", InstagramContactDisplay{Name: "Ana 💜 Ürsula", Handle: "ana_ü"}, "Ana 💜 Ürsula", "@ana_ü"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotHandle := instagramDisplayNames(tc.in)
			if gotName != tc.wantName {
				t.Errorf("name = %q, want %q", gotName, tc.wantName)
			}
			if gotHandle != tc.wantHandle {
				t.Errorf("handle = %q, want %q", gotHandle, tc.wantHandle)
			}
		})
	}
}

// A long handle must not be truncated or corrupted on its way to the UI; the UI
// owns presentation.
func TestInstagramDisplayNames_LongValuesPassThrough(t *testing.T) {
	long := strings.Repeat("a", 300)
	name, handle := instagramDisplayNames(InstagramContactDisplay{Handle: long})
	if handle != "@"+long || name != "@"+long {
		t.Error("long handle was altered")
	}
}

// ---------------------------------------------------------------- window state

// Registering an Instagram adapter must not make it reachable for WhatsApp:
// WhatsApp keeps its own lead/business-phone window rule. Asserted through the
// resolver rather than the full call, because the WhatsApp branch legitimately
// requires its repository (always wired in production).
func TestAdapterFor_InstagramAdapterNeverServesOtherChannels(t *testing.T) {
	expires := time.Now().Add(6 * time.Hour)
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType: shared.EntryTypeInstagram,
		open:      true,
		expiresAt: &expires,
	}))

	if a := svc.adapterFor("whatsapp"); a != nil {
		t.Error("an Instagram adapter must never be resolved for WhatsApp")
	}
	for _, entryType := range []string{"voice", "support", "", "INSTAGRAM"} {
		if a := svc.adapterFor(entryType); a != nil {
			t.Errorf("entry type %q must not resolve to the Instagram adapter", entryType)
		}
	}
	if a := svc.adapterFor("instagram"); a == nil {
		t.Error("instagram must resolve to its adapter")
	}

	// With no registry at all — a WhatsApp-only deployment — nothing resolves and
	// nothing panics.
	if a := (&HistoryProviderService{}).adapterFor("instagram"); a != nil {
		t.Error("no adapter should resolve without a registry")
	}
}

// An unknown channel with no adapter fails closed rather than defaulting open.
func TestGetWindowStatusForEntry_UnknownChannelFailsClosed(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType: shared.EntryTypeInstagram,
		open:      true,
	}))
	for _, entryType := range []string{"support", "email", "", "INSTAGRAM"} {
		if open, _ := svc.GetWindowStatusForEntry("x", entryType); open {
			t.Errorf("entry type %q must fail closed", entryType)
		}
	}
}

// A closed Instagram window (24h elapsed) must be reported closed so the
// composer can explain why a reply is blocked.
func TestGetWindowStatusForEntry_InstagramClosedWindow(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType: shared.EntryTypeInstagram,
		open:      false,
	}))
	open, expiresAt := svc.GetWindowStatusForEntry("ig-1", "instagram")
	if open {
		t.Error("an elapsed Instagram window must report closed")
	}
	if expiresAt != nil {
		t.Errorf("a closed window should carry no expiry, got %v", expiresAt)
	}
}

// ---------------------------------------------------------------- entry info

// GetEntryInfo is what fills the conversation header. WhatsApp must keep its own
// path, and an unsupported type must still be rejected.
func TestGetEntryInfo_ChannelRouting(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	// Unsupported channels are still rejected — adding Instagram did not open the
	// switch to everything.
	for _, entryType := range []string{"support", "email", "", "Instagram"} {
		if _, _, _, _, _, _, err := svc.GetEntryInfo("x", entryType); err == nil {
			t.Errorf("entry type %q should be rejected", entryType)
		}
	}

	// An Instagram conversation the repository cannot resolve surfaces the error
	// rather than a blank header.
	if _, _, _, _, _, _, err := svc.GetEntryInfo("unknown-conv", "instagram"); err == nil {
		t.Error("an unresolvable conversation should return an error")
	}
}

func TestGetEntryInfo_InstagramLookupFailurePropagates(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(&fakeInstagramContacts{err: errors.New("meta down")})
	if _, _, _, _, _, _, err := svc.GetEntryInfo("conv-1", "instagram"); err == nil {
		t.Error("a lookup failure must propagate, not yield an empty header")
	}
}

// ---------------------------------------------------------------- concurrency

// The inbox list is served concurrently per connected operator; hydration must
// be safe under -race with a shared service instance.
func TestHydrateInstagramSenders_ConcurrentPages(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(&concurrentSafeContacts{inner: igContactsFixture()})

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			entries := []conversation.InboxEntry{
				{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1"},
				{EntryID: "wa-1", EntryType: "whatsapp", LeadID: "lead-1", LeadName: "Ana"},
			}
			svc.hydrateInstagramSenders(entries)
			if entries[0].LeadName != "Maria Silva" || entries[1].LeadName != "Ana" {
				t.Errorf("bad hydration under concurrency: %+v", entries)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

// concurrentSafeContacts avoids the call counter's data race in the shared fake.
type concurrentSafeContacts struct{ inner *fakeInstagramContacts }

func (c *concurrentSafeContacts) ContactsByIDs(ctx context.Context, ids []string) (map[string]InstagramContactDisplay, error) {
	out := map[string]InstagramContactDisplay{}
	for _, id := range ids {
		if v, ok := c.inner.byID[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func (c *concurrentSafeContacts) ContactForConversation(ctx context.Context, id string) (InstagramContactDisplay, string, error) {
	return c.inner.ContactForConversation(ctx, id)
}

// --- entry_update regression (reported in production) ---
//
// An entry_update broadcast rebuilds a single row through GetInboxEntry, a path
// separate from the two list paths. It resolved the name through the lead
// repository, which cannot resolve an Instagram contact, so the conversation's
// name blanked every time a new message arrived.

func TestHydrateInstagramSenders_ReplacesRawProviderIDSender(t *testing.T) {
	const igsid = "17841458366137975"
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(&fakeInstagramContacts{
		byID: map[string]InstagramContactDisplay{
			"contact-1": {ContactID: "contact-1", Ref: igsid, Handle: "mariasilva", Name: "Maria Silva", PictureURL: "pic"},
		},
	})

	entries := []conversation.InboxEntry{
		// Story replies/mentions fall through the sender resolver's default
		// branch, which returns the raw ref: it must not reach the UI as a name.
		{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1", LastMessageSender: igsid},
		// A blank label is filled too.
		{EntryID: "ig-2", EntryType: "instagram", LeadID: "contact-1", LastMessageSender: ""},
		// An operator's name is authoritative and must survive.
		{EntryID: "ig-3", EntryType: "instagram", LeadID: "contact-1", LastMessageSender: "jose", LastMessageSenderAvatar: "jose.jpg"},
	}
	svc.hydrateInstagramSenders(entries)

	if entries[0].LastMessageSender != "Maria Silva" {
		t.Errorf("raw IGSID leaked as sender name: %q", entries[0].LastMessageSender)
	}
	if entries[1].LastMessageSender != "Maria Silva" {
		t.Errorf("blank sender not filled: %q", entries[1].LastMessageSender)
	}
	if entries[2].LastMessageSender != "jose" || entries[2].LastMessageSenderAvatar != "jose.jpg" {
		t.Errorf("operator sender overwritten: %+v", entries[2])
	}
	// The row identity is hydrated in every case.
	for i := range entries {
		if entries[i].LeadName != "Maria Silva" {
			t.Errorf("entry %d name = %q", i, entries[i].LeadName)
		}
	}
}

// Hydration must be idempotent: entry_update fires repeatedly for an active
// conversation, and each rebuild must produce the same row.
func TestHydrateInstagramSenders_Idempotent(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1"}}
	svc.hydrateInstagramSenders(entries)
	first := entries[0]
	for i := 0; i < 5; i++ {
		svc.hydrateInstagramSenders(entries)
	}
	if entries[0].LeadName != first.LeadName ||
		entries[0].LeadNumber != first.LeadNumber ||
		entries[0].LastMessageSender != first.LastMessageSender {
		t.Errorf("hydration is not idempotent:\n first %+v\n later %+v", first, entries[0])
	}
	// The handle must not accumulate '@' prefixes across rebuilds.
	if strings.HasPrefix(entries[0].LeadNumber, "@@") {
		t.Errorf("handle prefix accumulated: %q", entries[0].LeadNumber)
	}
}
