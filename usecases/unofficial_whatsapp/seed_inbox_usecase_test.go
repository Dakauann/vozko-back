package unofficial_whatsapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// fakePlaceholderWriter stands in for the conversation message repository,
// recording what the seeding wrote and how many messages each conversation
// already had.
type fakePlaceholderWriter struct {
	mu sync.Mutex
	// counts is the existing message count per entry id. Absent means zero.
	counts map[string]int64
	// countErr, when set for an entry, makes the count fail. A conversation
	// whose history cannot be read must be left alone, never assumed empty.
	countErr  map[string]error
	created   []*conversation.Message
	createErr error
	// failOnText makes one specific message fail to write, so a partially
	// written thread is testable.
	failOnText string
}

func newFakePlaceholderWriter() *fakePlaceholderWriter {
	return &fakePlaceholderWriter{counts: map[string]int64{}, countErr: map[string]error{}}
}

func (f *fakePlaceholderWriter) CountByEntry(entryID string, _ shared.EntryType) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.countErr[entryID]; ok {
		return 0, err
	}
	return f.counts[entryID], nil
}

func (f *fakePlaceholderWriter) Create(message *conversation.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	if f.failOnText != "" && message.Text == f.failOnText {
		return errors.New("write refused")
	}
	f.created = append(f.created, message)
	f.counts[message.EntryID]++
	return nil
}

func (f *fakePlaceholderWriter) writes() []*conversation.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*conversation.Message(nil), f.created...)
}

// fakeLeadLinker is the CRM bridge. Seeding must take the same one the inbound
// path takes, so the contact it creates is the lead the import created.
type fakeLeadLinker struct {
	mu      sync.Mutex
	byPhone map[string]string
	err     error
}

func newFakeLeadLinker() *fakeLeadLinker {
	return &fakeLeadLinker{byPhone: map[string]string{}}
}

func (f *fakeLeadLinker) EnsureLeadForPhone(_ context.Context, _, phone, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	id := "lead-" + phone
	f.byPhone[phone] = id
	return id, nil
}

func seedInstance(id string, status uw.Status, created time.Time) *uw.Instance {
	return &uw.Instance{
		ID: id, WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: status, CreatedAt: created,
	}
}

func newSeedUseCase(
	t *testing.T,
	instances []*uw.Instance,
	writer *fakePlaceholderWriter,
) (*SeedInboxUseCase, *fakeContactRepo, *fakeConversationRepo) {
	t.Helper()
	instanceRepo := newFakeInstanceRepo(instances...)
	contacts := newFakeContactRepo()
	conversations := newFakeConversationRepo()
	// Nil scripter and nil balance checker: this file covers the behaviour a
	// deployment without an AI service gets, which must be exactly what seeding
	// did before scripting existed.
	uc := NewSeedInboxUseCase(instanceRepo, contacts, conversations, newFakeLeadLinker(), writer, nil, nil)
	return uc, contacts, conversations
}

// The happy path: a number nobody has written to becomes a contact, a lead, a
// conversation and exactly one placeholder, which is the whole reason the
// conversation renders in the inbox at all.
func TestSeedInboxOpensAConversationAndWritesOnePlaceholder(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, contacts, conversations := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "+55 11 99999-9999", Name: "Ana"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.AlreadyActive != 0 || out.Failed != 0 {
		t.Fatalf("outcome = %+v, want 1 seeded", out)
	}

	if len(contacts.created) != 1 {
		t.Fatalf("contacts created = %d, want 1", len(contacts.created))
	}
	// The JID is built from the number rather than verified, so it must be the
	// canonical user form. A contact stored under anything else is a contact the
	// inbound webhook cannot match.
	if got, want := contacts.created[0].JID, "5511999999999@s.whatsapp.net"; got != want {
		t.Errorf("contact JID = %q, want %q", got, want)
	}
	if got, want := contacts.created[0].PhoneNumber, "5511999999999"; got != want {
		t.Errorf("contact phone = %q, want %q", got, want)
	}
	// The lead bridge is what makes the inbox row render the lead's name rather
	// than a bare number, and it is why this reuses ConversationResolver.
	if contacts.created[0].LeadID == nil {
		t.Error("contact was not bridged to a lead")
	}
	if len(conversations.created) != 1 {
		t.Fatalf("conversations created = %d, want 1", len(conversations.created))
	}

	writes := writer.writes()
	if len(writes) != 1 {
		t.Fatalf("placeholders written = %d, want 1", len(writes))
	}
	msg := writes[0]
	if msg.EntryID != conversations.created[0].ID {
		t.Errorf("placeholder EntryID = %q, want %q", msg.EntryID, conversations.created[0].ID)
	}
	if msg.EntryType != shared.EntryTypeUnofficialWhatsApp {
		t.Errorf("placeholder EntryType = %q", msg.EntryType)
	}
	// System, because it is the one type every AI history builder already skips
	// and the one type that is not counted as inbound. Any other type would put
	// a message the lead never sent into the agent's context and a badge on the
	// inbox row.
	if msg.MessageType != conversation.MessageTypeSystem {
		t.Errorf("placeholder MessageType = %q, want %q", msg.MessageType, conversation.MessageTypeSystem)
	}
	if msg.Text != "" {
		t.Errorf("placeholder Text = %q, want empty", msg.Text)
	}
	// Read on arrival. System is not an inbound type so it cannot raise the
	// unread count anyway, but a row that is unread-by-default is one schema
	// change away from lighting up every seeded conversation.
	if !msg.Read {
		t.Error("placeholder should be marked read")
	}
}

// The rule that protects live chats: a conversation that already has history
// must not get a placeholder, because that writes a blank pill into a real
// thread and bumps last_message_at, moving an old conversation to the top of
// the inbox for no reason.
func TestSeedInboxNeverTouchesAConversationThatHasMessages(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, conversations := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)

	existing := &uw.Conversation{
		ID: "conv-live", WorkspaceID: "ws-1", InstanceID: "inst-1",
		ContactID: "contact-5511999999999@s.whatsapp.net",
		ChatID:    "5511999999999@s.whatsapp.net",
	}
	conversations.convs[existing.ID] = existing
	writer.counts[existing.ID] = 12

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 0 || out.AlreadyActive != 1 {
		t.Fatalf("outcome = %+v, want 1 already active", out)
	}
	if writes := writer.writes(); len(writes) != 0 {
		t.Fatalf("wrote %d placeholders into a live conversation", len(writes))
	}
}

// Re-importing the same file is the single most likely way this runs twice.
// The second pass must find the conversation the first one made, see the
// placeholder it already wrote, and do nothing.
func TestSeedInboxIsIdempotent(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, _ := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)

	req := uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	}
	if _, err := uc.Execute(context.Background(), req); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	out, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if out.Seeded != 0 || out.AlreadyActive != 1 {
		t.Fatalf("second run outcome = %+v, want 1 already active", out)
	}
	if writes := writer.writes(); len(writes) != 1 {
		t.Fatalf("placeholders written = %d, want 1 across two runs", len(writes))
	}
}

// A conversation whose history could not be read is left alone. Treating a
// failed count as "empty" is how a live thread gets a placeholder written into
// it during a database blip.
func TestSeedInboxSkipsWhenTheHistoryCountFails(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, conversations := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)

	existing := &uw.Conversation{
		ID: "conv-unreadable", WorkspaceID: "ws-1", InstanceID: "inst-1",
		ContactID: "contact-5511999999999@s.whatsapp.net",
		ChatID:    "5511999999999@s.whatsapp.net",
	}
	conversations.convs[existing.ID] = existing
	writer.countErr[existing.ID] = errors.New("connection reset")

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Failed != 1 || out.Seeded != 0 {
		t.Fatalf("outcome = %+v, want 1 failed", out)
	}
	if writes := writer.writes(); len(writes) != 0 {
		t.Fatalf("wrote %d placeholders despite an unreadable history", len(writes))
	}
}

// One bad row must not cost the rest of the batch their inbox entries, the same
// stance PrepareImport and bridgeContactLead already take.
func TestSeedInboxKeepsGoingAfterOneTargetFails(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, conversations := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)

	broken := &uw.Conversation{
		ID: "conv-broken", WorkspaceID: "ws-1", InstanceID: "inst-1",
		ContactID: "contact-5511888888888@s.whatsapp.net",
		ChatID:    "5511888888888@s.whatsapp.net",
	}
	conversations.convs[broken.ID] = broken
	writer.countErr[broken.ID] = errors.New("connection reset")

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets: []uw.SeedTarget{
			{Number: "5511888888888"},
			{Number: "5511999999999"},
			{Number: "5511777777777"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 2 || out.Failed != 1 {
		t.Fatalf("outcome = %+v, want 2 seeded and 1 failed", out)
	}
}

// The instance is chosen, not configured, so the choice has to be stable.
// Oldest-first means adding a second number does not silently move where the
// next import's conversations land.
func TestSeedInboxPicksTheOldestConnectedInstance(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, conversations := newSeedUseCase(t, []*uw.Instance{
		seedInstance("inst-new", uw.StatusConnected, time.Unix(300, 0)),
		seedInstance("inst-old", uw.StatusConnected, time.Unix(100, 0)),
		// Disconnected and banned numbers are not candidates: a conversation on
		// a dead session is an inbox row that cannot be answered.
		seedInstance("inst-dead", uw.StatusDisconnected, time.Unix(50, 0)),
		seedInstance("inst-banned", uw.StatusBanned, time.Unix(10, 0)),
	}, writer)

	if _, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(conversations.created) != 1 {
		t.Fatalf("conversations created = %d, want 1", len(conversations.created))
	}
	if got := conversations.created[0].InstanceID; got != "inst-old" {
		t.Fatalf("conversation on instance %q, want %q", got, "inst-old")
	}
}

// Nothing to seed onto. Refusing beats writing conversations against a
// disconnected number and reporting success.
func TestSeedInboxRefusesWithoutAConnectedInstance(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, _ := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-dead", uw.StatusDisconnected, time.Unix(100, 0))}, writer)

	_, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	})
	if !errors.Is(err, ErrNoConnectedInstance) {
		t.Fatalf("err = %v, want ErrNoConnectedInstance", err)
	}
	if writes := writer.writes(); len(writes) != 0 {
		t.Fatalf("wrote %d placeholders with no connected instance", len(writes))
	}
}

// Normalization is the use case's own responsibility, not the caller's: this
// runs off a queue message, and a producer from an older build must not be able
// to smuggle a group id or a four-digit row past it.
func TestSeedInboxNormalizesItsRequest(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, contacts, _ := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets: []uw.SeedTarget{
			{Number: "120363427766359853@g.us"},
			{Number: "1234"},
			{Number: "+55 (11) 99999-9999"},
			{Number: "5511999999999"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 {
		t.Fatalf("outcome = %+v, want 1 seeded", out)
	}
	if len(contacts.created) != 1 {
		t.Fatalf("contacts created = %d, want 1", len(contacts.created))
	}
	if contacts.created[0].IsGroup {
		t.Error("a group id was seeded as a person")
	}
}
