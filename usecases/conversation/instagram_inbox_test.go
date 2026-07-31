package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// fakeInstagramContacts is a stand-in for the Instagram repositories behind the
// sender-identity port.
type fakeInstagramContacts struct {
	byID     map[string]InstagramContactDisplay
	byConv   map[string]string
	wsForRef string
	err      error
	calls    int
}

func (f *fakeInstagramContacts) ContactsByIDs(_ context.Context, ids []string) (map[string]InstagramContactDisplay, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]InstagramContactDisplay{}
	for _, id := range ids {
		if c, ok := f.byID[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func (f *fakeInstagramContacts) ContactForConversation(_ context.Context, conversationID string) (InstagramContactDisplay, string, error) {
	if f.err != nil {
		return InstagramContactDisplay{}, "", f.err
	}
	contactID, ok := f.byConv[conversationID]
	if !ok {
		return InstagramContactDisplay{}, "", errors.New("conversation not found")
	}
	return f.byID[contactID], f.wsForRef, nil
}

func igContactsFixture() *fakeInstagramContacts {
	return &fakeInstagramContacts{
		byID: map[string]InstagramContactDisplay{
			"contact-1": {ContactID: "contact-1", Handle: "mariasilva", Name: "Maria Silva", PictureURL: "https://cdn/avatar1.jpg"},
			"contact-2": {ContactID: "contact-2", Handle: "joao_p"},
			"contact-3": {ContactID: "contact-3"},
		},
		byConv:   map[string]string{"conv-1": "contact-1"},
		wsForRef: "ws-1",
	}
}

func TestHydrateInstagramSenders(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	entries := []conversation.InboxEntry{
		// Enriched profile: name + handle + avatar.
		{EntryID: "conv-1", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-1"},
		// Handle only (profile not enriched yet): the handle stands in for the name.
		{EntryID: "conv-2", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-2"},
		// Nothing known: must still render a usable label rather than blank.
		{EntryID: "conv-3", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-3"},
		// A WhatsApp row must be left exactly as it was.
		{EntryID: "wa-1", EntryType: string(shared.EntryTypeWhatsApp), LeadID: "lead-9", LeadName: "Lead Nine", LeadNumber: "+5511999999999"},
	}

	svc.hydrateInstagramSenders(entries)

	if entries[0].LeadName != "Maria Silva" {
		t.Errorf("entry 0 name = %q, want %q", entries[0].LeadName, "Maria Silva")
	}
	if entries[0].LeadNumber != "@mariasilva" {
		t.Errorf("entry 0 handle = %q, want %q", entries[0].LeadNumber, "@mariasilva")
	}
	if entries[0].LeadPicture != "https://cdn/avatar1.jpg" {
		t.Errorf("entry 0 picture = %q", entries[0].LeadPicture)
	}
	// The last-message sender label was empty before hydration, so it adopts the
	// contact — otherwise the inbox row shows a message with no author.
	if entries[0].LastMessageSender != "Maria Silva" {
		t.Errorf("entry 0 sender = %q, want %q", entries[0].LastMessageSender, "Maria Silva")
	}

	if entries[1].LeadName != "@joao_p" {
		t.Errorf("entry 1 name = %q, want handle fallback %q", entries[1].LeadName, "@joao_p")
	}
	if entries[2].LeadName != "Instagram" {
		t.Errorf("entry 2 name = %q, want %q", entries[2].LeadName, "Instagram")
	}

	if entries[3].LeadName != "Lead Nine" || entries[3].LeadNumber != "+5511999999999" {
		t.Errorf("whatsapp entry was mutated: %+v", entries[3])
	}
}

func TestHydrateInstagramSendersBatchesOneQuery(t *testing.T) {
	fake := igContactsFixture()
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(fake)

	entries := []conversation.InboxEntry{
		{EntryID: "a", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-1"},
		{EntryID: "b", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-2"},
		{EntryID: "c", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-1"},
	}
	svc.hydrateInstagramSenders(entries)

	if fake.calls != 1 {
		t.Fatalf("contact lookup ran %d times, want 1 batched call", fake.calls)
	}
	if entries[2].LeadName != "Maria Silva" {
		t.Errorf("repeated contact not hydrated: %q", entries[2].LeadName)
	}
}

func TestHydrateInstagramSendersDegradesGracefully(t *testing.T) {
	entries := []conversation.InboxEntry{
		{EntryID: "conv-1", EntryType: string(shared.EntryTypeInstagram), LeadID: "contact-1"},
	}

	// No lookup wired: must not panic, and must leave the row untouched.
	(&HistoryProviderService{}).hydrateInstagramSenders(entries)
	if entries[0].LeadName != "" {
		t.Errorf("expected no hydration without lookup, got %q", entries[0].LeadName)
	}

	// A failing lookup must never break the inbox listing.
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(&fakeInstagramContacts{err: errors.New("db down")})
	svc.hydrateInstagramSenders(entries)
	if entries[0].LeadName != "" {
		t.Errorf("expected no hydration on error, got %q", entries[0].LeadName)
	}
}

func TestGetEntryInfoInstagram(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetInstagramContacts(igContactsFixture())

	name, handle, picture, _, _, automation, err := svc.GetEntryInfo("conv-1", string(shared.EntryTypeInstagram))
	if err != nil {
		t.Fatalf("GetEntryInfo returned error: %v", err)
	}
	if name != "Maria Silva" || handle != "@mariasilva" || picture != "https://cdn/avatar1.jpg" {
		t.Errorf("got (%q, %q, %q)", name, handle, picture)
	}
	if !automation {
		t.Error("automation should default to enabled")
	}

	// Without the port the header cannot be resolved; an error is correct, a
	// silent blank header is not.
	if _, _, _, _, _, _, err := (&HistoryProviderService{}).GetEntryInfo("conv-1", string(shared.EntryTypeInstagram)); err == nil {
		t.Error("expected an error when the contact lookup is not configured")
	}
}

// fakeWindowAdapter is a minimal ChannelAdapter that reports a fixed window.
type fakeWindowAdapter struct {
	entryType  shared.EntryType
	open       bool
	expiresAt  *time.Time
	resolveErr error
}

func (a *fakeWindowAdapter) EntryType() shared.EntryType { return a.entryType }

func (a *fakeWindowAdapter) ResolveEntry(_ context.Context, entryID string) (*conversation.EntryContext, error) {
	if a.resolveErr != nil {
		return nil, a.resolveErr
	}
	return &conversation.EntryContext{EntryID: entryID, EntryType: a.entryType}, nil
}

func (a *fakeWindowAdapter) WindowState(_ context.Context, _ *conversation.EntryContext) (bool, *time.Time, error) {
	return a.open, a.expiresAt, nil
}

func (a *fakeWindowAdapter) SendText(_ context.Context, _ *conversation.EntryContext, _ conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

func (a *fakeWindowAdapter) SendMedia(_ context.Context, _ *conversation.EntryContext, _ conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

// The composer reads this window state; if it reported closed for Instagram the
// operator could not reply even inside the 24h window.
func TestGetWindowStatusForEntryUsesChannelAdapter(t *testing.T) {
	expires := time.Now().Add(12 * time.Hour).UTC()
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType: shared.EntryTypeInstagram,
		open:      true,
		expiresAt: &expires,
	}))

	open, expiresAt := svc.GetWindowStatusForEntry("conv-1", string(shared.EntryTypeInstagram))
	if !open {
		t.Error("expected the Instagram window to be reported open")
	}
	if expiresAt == nil || !expiresAt.Equal(expires) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, expires)
	}
}

func TestGetWindowStatusForEntryClosedWithoutAdapter(t *testing.T) {
	// No adapter registered: a channel we cannot evaluate must fail closed.
	open, expiresAt := (&HistoryProviderService{}).GetWindowStatusForEntry("conv-1", string(shared.EntryTypeInstagram))
	if open || expiresAt != nil {
		t.Errorf("expected a closed window without an adapter, got open=%v expiresAt=%v", open, expiresAt)
	}

	// A resolve failure must also fail closed rather than claim the window is open.
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{
		entryType:  shared.EntryTypeInstagram,
		open:       true,
		resolveErr: errors.New("entry gone"),
	}))
	if open, _ := svc.GetWindowStatusForEntry("conv-1", string(shared.EntryTypeInstagram)); open {
		t.Error("expected a closed window when the entry cannot be resolved")
	}
}
