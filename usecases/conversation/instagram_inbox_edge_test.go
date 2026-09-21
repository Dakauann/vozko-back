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

	svc.hydrateContactSenders(entries)

	if fake.calls != 0 {
		t.Errorf("contact lookup ran %d times for a WhatsApp-only page; want 0 (no wasted query)", fake.calls)
	}
	if len(entries) != len(before) {
		t.Fatalf("entry count changed: %d -> %d", len(before), len(entries))
	}
	for i := range before {
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

func TestHydrateInstagramSenders_EntryTypeIsTheDiscriminator(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{
		{EntryID: "wa-1", EntryType: "whatsapp", LeadID: "contact-1", LeadName: "Real Lead", LeadNumber: "+5511999999999"},
	}
	svc.hydrateContactSenders(entries)

	if entries[0].LeadName != "Real Lead" || entries[0].LeadNumber != "+5511999999999" {
		t.Errorf("a WhatsApp row was hydrated from Instagram data: %+v", entries[0])
	}
}

func TestHydrateInstagramSenders_MixedChannelPage(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{
		{EntryID: "wa-1", EntryType: "whatsapp", LeadID: "lead-1", LeadName: "Ana", LeadNumber: "+5511111111111"},
		{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1"},
		{EntryID: "sup-1", EntryType: "support", LeadID: "lead-3", LeadName: "Carla"},
		{EntryID: "ig-2", EntryType: "instagram", LeadID: "contact-2"},
	}
	svc.hydrateContactSenders(entries)

	if entries[0].LeadName != "Ana" || entries[2].LeadName != "Carla" {
		t.Error("non-Instagram rows must be preserved in a mixed page")
	}
	if entries[1].LeadName != "Maria Silva" || entries[3].LeadName != "@joao_p" {
		t.Errorf("Instagram rows not hydrated in a mixed page: %q, %q", entries[1].LeadName, entries[3].LeadName)
	}
}

func TestHydrateInstagramSenders_EdgeCaseInputs(t *testing.T) {
	(&HistoryProviderService{}).hydrateContactSenders(nil)
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())
	svc.hydrateContactSenders([]conversation.InboxEntry{})

	fake := igContactsFixture()
	svc2 := &HistoryProviderService{}
	svc2.SetInstagramContacts(fake)
	entries := []conversation.InboxEntry{{EntryID: "ig-1", EntryType: "instagram", LeadID: ""}}
	svc2.hydrateContactSenders(entries)
	if fake.calls != 0 {
		t.Errorf("lookup ran for a row with no contact id (%d calls)", fake.calls)
	}

	entries = []conversation.InboxEntry{{EntryID: "ig-1", EntryType: "instagram", LeadID: "ghost", LeadName: "prior"}}
	svc2.hydrateContactSenders(entries)
	if entries[0].LeadName != "prior" {
		t.Errorf("unknown contact should leave the row untouched, got %q", entries[0].LeadName)
	}
}

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
	svc.hydrateContactSenders(entries)

	if entries[0].LastMessageSender != "operator-jose" {
		t.Errorf("operator sender label overwritten: %q", entries[0].LastMessageSender)
	}
	if entries[0].LastMessageSenderAvatar != "https://cdn/jose.jpg" {
		t.Errorf("operator avatar overwritten: %q", entries[0].LastMessageSenderAvatar)
	}
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
			gotName, gotHandle := contactDisplayNames(shared.EntryTypeInstagram, tc.in)
			if gotName != tc.wantName {
				t.Errorf("name = %q, want %q", gotName, tc.wantName)
			}
			if gotHandle != tc.wantHandle {
				t.Errorf("handle = %q, want %q", gotHandle, tc.wantHandle)
			}
		})
	}
}

func TestInstagramDisplayNames_LongValuesPassThrough(t *testing.T) {
	long := strings.Repeat("a", 300)
	name, handle := contactDisplayNames(shared.EntryTypeInstagram, InstagramContactDisplay{Handle: long})
	if handle != "@"+long || name != "@"+long {
		t.Error("long handle was altered")
	}
}

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

	if a := (&HistoryProviderService{}).adapterFor("instagram"); a != nil {
		t.Error("no adapter should resolve without a registry")
	}
}

func TestGetWindowStatusForEntry_UnknownChannelFailsClosed(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType: shared.EntryTypeInstagram,
		open:      true,
	}))
	for _, entryType := range []string{"support", "email", "", "INSTAGRAM"} {
		if svc.GetWindowStatusForEntry("x", entryType).Open {
			t.Errorf("entry type %q must fail closed", entryType)
		}
	}
}

func TestGetWindowStatusForEntry_InstagramClosedWindow(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType: shared.EntryTypeInstagram,
		open:      false,
	}))
	window := svc.GetWindowStatusForEntry("ig-1", "instagram")
	open, expiresAt := window.Open, window.ExpiresAt
	if open {
		t.Error("an elapsed Instagram window must report closed")
	}
	if expiresAt != nil {
		t.Errorf("a closed window should carry no expiry, got %v", expiresAt)
	}
}

func TestGetEntryInfo_ChannelRouting(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	for _, entryType := range []string{"support", "email", "", "Instagram"} {
		if _, _, _, _, _, _, err := svc.GetEntryInfo("x", entryType); err == nil {
			t.Errorf("entry type %q should be rejected", entryType)
		}
	}

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
			svc.hydrateContactSenders(entries)
			if entries[0].LeadName != "Maria Silva" || entries[1].LeadName != "Ana" {
				t.Errorf("bad hydration under concurrency: %+v", entries)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

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

func (c *concurrentSafeContacts) AuthorsByHandle(context.Context, string, []string) (map[string]InstagramContactDisplay, error) {
	return nil, nil
}

func (c *concurrentSafeContacts) ContactForConversation(ctx context.Context, id string) (InstagramContactDisplay, string, error) {
	return c.inner.ContactForConversation(ctx, id)
}

func TestHydrateInstagramSenders_ReplacesRawProviderIDSender(t *testing.T) {
	const igsid = "17841458366137975"
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(&fakeInstagramContacts{
		byID: map[string]InstagramContactDisplay{
			"contact-1": {ContactID: "contact-1", Ref: igsid, Handle: "mariasilva", Name: "Maria Silva", PictureURL: "pic"},
		},
	})

	entries := []conversation.InboxEntry{
		{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1", LastMessageSender: igsid},
		{EntryID: "ig-2", EntryType: "instagram", LeadID: "contact-1", LastMessageSender: ""},
		{EntryID: "ig-3", EntryType: "instagram", LeadID: "contact-1", LastMessageSender: "jose", LastMessageSenderAvatar: "jose.jpg"},
	}
	svc.hydrateContactSenders(entries)

	if entries[0].LastMessageSender != "Maria Silva" {
		t.Errorf("raw IGSID leaked as sender name: %q", entries[0].LastMessageSender)
	}
	if entries[1].LastMessageSender != "Maria Silva" {
		t.Errorf("blank sender not filled: %q", entries[1].LastMessageSender)
	}
	if entries[2].LastMessageSender != "jose" || entries[2].LastMessageSenderAvatar != "jose.jpg" {
		t.Errorf("operator sender overwritten: %+v", entries[2])
	}
	for i := range entries {
		if entries[i].LeadName != "Maria Silva" {
			t.Errorf("entry %d name = %q", i, entries[i].LeadName)
		}
	}
}

func TestHydrateInstagramSenders_Idempotent(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{{EntryID: "ig-1", EntryType: "instagram", LeadID: "contact-1"}}
	svc.hydrateContactSenders(entries)
	first := entries[0]
	for i := 0; i < 5; i++ {
		svc.hydrateContactSenders(entries)
	}
	if entries[0].LeadName != first.LeadName ||
		entries[0].LeadNumber != first.LeadNumber ||
		entries[0].LastMessageSender != first.LastMessageSender {
		t.Errorf("hydration is not idempotent:\n first %+v\n later %+v", first, entries[0])
	}
	if strings.HasPrefix(entries[0].LeadNumber, "@@") {
		t.Errorf("handle prefix accumulated: %q", entries[0].LeadNumber)
	}
}
