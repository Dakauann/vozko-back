package messageaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/usecases/messagestate"
)

var actionNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

type grantingAccess struct {
	permission bool
	entry      bool
	asked      []string
}

func (a *grantingAccess) HasWorkspacePermission(userID, workspaceID, resource, action string, _ bool) bool {
	a.asked = append(a.asked, resource+":"+action)
	return a.permission && userID != "" && workspaceID != ""
}

func (a *grantingAccess) CanAccessEntry(userID, workspaceID, entryID, entryType string, _ bool) bool {
	return a.entry && userID != "" && workspaceID != "" && entryID != "" && entryType != ""
}

type storedMessages struct {
	byID map[string]*conversation.Message
}

func (s *storedMessages) GetByID(id string) (*conversation.Message, error) {
	if m, ok := s.byID[id]; ok {
		copied := *m
		return &copied, nil
	}
	return nil, conversation.ErrMessageNotFound
}

type savingStore struct {
	saved []*conversation.Message
	fail  error
}

func (s *savingStore) Update(_ string, message *conversation.Message) error {
	if s.fail != nil {
		return s.fail
	}
	copied := *message
	s.saved = append(s.saved, &copied)
	return nil
}

type recordingAnnouncer struct {
	updated []string
}

func (a *recordingAnnouncer) BroadcastMessageUpdated(_, _ string, message *conversation.Message) {
	a.updated = append(a.updated, message.ID)
}

func (a *recordingAnnouncer) BroadcastEntryUpdate(string, string, *conversation.Message) {}

type actingChannel struct {
	conversation.ChannelAdapter
	entryType shared.EntryType
	calls     []string
	fail      error
}

func (c *actingChannel) EntryType() shared.EntryType { return c.entryType }

func (c *actingChannel) ResolveEntry(_ context.Context, entryID string) (*conversation.EntryContext, error) {
	return &conversation.EntryContext{EntryID: entryID, EntryType: c.entryType, ContactRef: "contact-1"}, nil
}

func (c *actingChannel) record(call string) error {
	if c.fail != nil {
		return c.fail
	}
	c.calls = append(c.calls, call)
	return nil
}

func (c *actingChannel) SendReaction(_ context.Context, _ *conversation.EntryContext, target, reaction string) error {
	return c.record("react:" + target + ":" + reaction)
}

func (c *actingChannel) RemoveReaction(_ context.Context, _ *conversation.EntryContext, target string) error {
	return c.record("unreact:" + target)
}

func (c *actingChannel) EditText(_ context.Context, _ *conversation.EntryContext, target, body string) error {
	return c.record("edit:" + target + ":" + body)
}

func (c *actingChannel) EditWindow() time.Duration { return 15 * time.Minute }

func (c *actingChannel) Retract(_ context.Context, _ *conversation.EntryContext, target string) error {
	return c.record("retract:" + target)
}

func (c *actingChannel) RetractWindow() time.Duration { return 48 * time.Hour }

type reactOnlyChannel struct {
	conversation.ChannelAdapter
}

func (reactOnlyChannel) EntryType() shared.EntryType { return shared.EntryTypeInstagram }

func (reactOnlyChannel) ResolveEntry(context.Context, string) (*conversation.EntryContext, error) {
	return &conversation.EntryContext{}, nil
}

func (reactOnlyChannel) SendReaction(context.Context, *conversation.EntryContext, string, string) error {
	return nil
}

func (reactOnlyChannel) RemoveReaction(context.Context, *conversation.EntryContext, string) error {
	return nil
}

type recordingSend struct {
	inputs []conversation.OperatorSendInput
	answer *conversation.Message
	err    error
}

func (s *recordingSend) Execute(_ context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	s.inputs = append(s.inputs, in)
	if s.err != nil {
		return nil, s.err
	}
	return s.answer, nil
}

type actionFixture struct {
	access    *grantingAccess
	messages  *storedMessages
	store     *savingStore
	announcer *recordingAnnouncer
	channel   *actingChannel
	send      *recordingSend
	uc        conversation.MessageActionUseCases
}

func newActionFixture(t *testing.T, messages ...*conversation.Message) *actionFixture {
	t.Helper()
	f := &actionFixture{
		access:    &grantingAccess{permission: true, entry: true},
		messages:  &storedMessages{byID: map[string]*conversation.Message{}},
		store:     &savingStore{},
		announcer: &recordingAnnouncer{},
		channel:   &actingChannel{entryType: shared.EntryTypeTelegram},
		send:      &recordingSend{answer: &conversation.Message{ID: "msg-new", ClientRequestID: "req-1"}},
	}
	for _, m := range messages {
		f.messages.byID[m.ID] = m
	}
	uc, err := NewUseCases(Deps{
		Access:   f.access,
		Messages: f.messages,
		Channels: Channels{
			Adapters: conversation.NewAdapterRegistry(f.channel, reactOnlyChannel{}),
			Official: reactOnlyChannel{},
		},
		Writer: messagestate.Writer{Messages: f.store, Announcer: f.announcer},
		Send:   f.send,
		Now:    func() time.Time { return actionNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.uc = uc
	return f
}

func providerID(id string) *string { return &id }

func ownTelegramText(id string, sentAt time.Time) *conversation.Message {
	return &conversation.Message{
		ID: id, EntryID: "entry-1", EntryType: shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeOperator, Text: "oi", ExternalMessageID: providerID("pm-" + id),
		SentBy: conversation.SentByPerson("user-ana"), DeliveryStatus: conversation.DeliveryStatusSent, CreatedAt: sentAt,
	}
}

func customerTelegramText(id string) *conversation.Message {
	return &conversation.Message{
		ID: id, EntryID: "entry-1", EntryType: shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeUserMessage, Text: "bom dia", ExternalMessageID: providerID("pm-" + id),
		SentBy: conversation.SentByContact("5511"), CreatedAt: actionNow.Add(-time.Hour),
	}
}

func target(messageID string) conversation.MessageTarget {
	return conversation.MessageTarget{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeTelegram, MessageID: messageID}
}

var ana = shared.Person{UserID: "user-ana"}

func (f *actionFixture) requireNothingChanged(t *testing.T) {
	t.Helper()
	if len(f.store.saved) != 0 || len(f.announcer.updated) != 0 {
		t.Fatalf("saved %d and announced %v; a refused action must change nothing", len(f.store.saved), f.announcer.updated)
	}
}

func requireError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
