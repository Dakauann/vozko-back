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

type fakePlaceholderWriter struct {
	mu         sync.Mutex
	counts     map[string]int64
	countErr   map[string]error
	created    []*conversation.Message
	createErr  error
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
	uc := NewSeedInboxUseCase(instanceRepo, contacts, conversations, newFakeLeadLinker(), writer, nil, nil)
	return uc, contacts, conversations
}

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
	if got, want := contacts.created[0].JID, "5511999999999@s.whatsapp.net"; got != want {
		t.Errorf("contact JID = %q, want %q", got, want)
	}
	if got, want := contacts.created[0].PhoneNumber, "5511999999999"; got != want {
		t.Errorf("contact phone = %q, want %q", got, want)
	}
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
	if msg.MessageType != conversation.MessageTypeSystem {
		t.Errorf("placeholder MessageType = %q, want %q", msg.MessageType, conversation.MessageTypeSystem)
	}
	if msg.Text != "" {
		t.Errorf("placeholder Text = %q, want empty", msg.Text)
	}
	if !msg.Read {
		t.Error("placeholder should be marked read")
	}
}

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

func TestSeedInboxPicksTheOldestConnectedInstance(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, conversations := newSeedUseCase(t, []*uw.Instance{
		seedInstance("inst-new", uw.StatusConnected, time.Unix(300, 0)),
		seedInstance("inst-old", uw.StatusConnected, time.Unix(100, 0)),
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
