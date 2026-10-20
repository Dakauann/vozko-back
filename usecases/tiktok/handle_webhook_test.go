package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
)

type fakeMedia struct {
	mu     sync.Mutex
	stored []conversation.StoreMediaInput
	err    error
}

func (f *fakeMedia) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.stored = append(f.stored, in)
	return &conversation.ConversationMedia{ID: in.ID, EntryID: in.EntryID, EntryType: in.EntryType, Type: in.Type, MimeType: in.MimeType, URL: "https://media.example.com/" + in.Key}, nil
}

type fakeWatermarks struct {
	calls []time.Time
}

func (f *fakeWatermarks) MarkOutboundStatusUpTo(entryID string, entryType shared.EntryType, status conversation.DeliveryStatus, upTo time.Time) (int64, error) {
	f.calls = append(f.calls, upTo)
	return 1, nil
}

type fakeMessageStore struct {
	mu       sync.Mutex
	messages map[string]*conversation.Message
	updated  int
}

func (f *fakeMessageStore) GetByEntryAndExternalMessageID(t shared.EntryType, entryID, externalID string) (*conversation.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.messages[entryID+":"+externalID]; ok {
		return m, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeMessageStore) Update(id string, m *conversation.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated++
	return nil
}

type fakeAnnouncer struct{}

func (fakeAnnouncer) BroadcastMessageUpdated(entryID, entryType string, m *conversation.Message) {}
func (fakeAnnouncer) BroadcastEntryUpdate(entryID, entryType string, m *conversation.Message)    {}

type fakeCommentSink struct {
	forgotten []string
}

func (f *fakeCommentSink) Forget(ctx context.Context, a *ttdomain.Account, commentID string) {
	f.forgotten = append(f.forgotten, commentID)
}

type fakeComments struct {
	mu      sync.Mutex
	writes  []ttdomain.CommentEventWrite
	applied bool
	byID    map[string]*ttdomain.Comment
	ours    []*ttdomain.Comment
	mirror  []*ttdomain.Comment
	leased  []*ttdomain.Comment
	result  map[string]string
}

func newFakeComments() *fakeComments {
	return &fakeComments{applied: true, byID: map[string]*ttdomain.Comment{}, result: map[string]string{}}
}

func (f *fakeComments) ApplyEvent(ctx context.Context, w ttdomain.CommentEventWrite) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, w)
	return f.applied, nil
}

func (f *fakeComments) RecordOurs(ctx context.Context, c *ttdomain.Comment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ours = append(f.ours, c)
	return nil
}

func (f *fakeComments) MirrorMany(ctx context.Context, comments []*ttdomain.Comment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mirror = append(f.mirror, comments...)
	for _, c := range comments {
		f.byID[c.CommentID] = c
	}
	return nil
}

func (f *fakeComments) ClaimLookups(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*ttdomain.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.leased
	f.leased = nil
	return out, nil
}

func (f *fakeComments) ResolveLookup(ctx context.Context, id string, a ttdomain.ResolvedAuthor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result[id] = "resolved"
	return nil
}

func (f *fakeComments) DeferLookup(ctx context.Context, id string, attempts int, next time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result[id] = "deferred"
	return nil
}

func (f *fakeComments) FailLookup(ctx context.Context, id string, attempts int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result[id] = "failed"
	return nil
}

func (f *fakeComments) MarkRulesDone(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.result[id] == "resolved" {
		f.result[id] = "resolved+rules"
	} else {
		f.result[id] = "rules"
	}
	return nil
}

func (f *fakeComments) FindByCommentID(ctx context.Context, accountID, commentID string) (*ttdomain.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byID[commentID]; ok {
		return c, nil
	}
	return nil, ttdomain.ErrCommentNotFound
}

func (f *fakeComments) FindByCommentIDs(ctx context.Context, accountID string, ids []string) ([]*ttdomain.Comment, error) {
	var out []*ttdomain.Comment
	for _, id := range ids {
		if c, err := f.FindByCommentID(ctx, accountID, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeComments) List(ctx context.Context, in ttdomain.ListCommentsInput) (*shared.PaginatedResult[*ttdomain.Comment], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*ttdomain.Comment
	for _, c := range f.byID {
		if in.VideoID == "" || c.VideoID == in.VideoID {
			items = append(items, c)
		}
	}
	return &shared.PaginatedResult[*ttdomain.Comment]{Items: items, TotalItems: int64(len(items)), Page: 1, PageSize: len(items), TotalPages: 1}, nil
}

func (f *fakeComments) SetVisibility(ctx context.Context, accountID, commentID string, v ttdomain.CommentVisibility, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byID[commentID]; ok {
		c.Visibility = v
	}
	return nil
}

func (f *fakeComments) SetLiked(ctx context.Context, accountID, commentID string, liked bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byID[commentID]; ok {
		c.LikedByUs = liked
	}
	return nil
}

type webhookHarness struct {
	h             *HandleWebhook
	account       *ttdomain.Account
	accounts      *fakeAccounts
	contacts      *fakeContacts
	conversations *fakeConversations
	comments      *fakeComments
	svc           *fakeServices
	history       *fakeHistory
	media         *fakeMedia
	watermarks    *fakeWatermarks
	messages      *fakeMessageStore
	assignments   *fakeAssignments
	dispatcher    *fakeDispatcher
	sink          *fakeCommentSink
	metrics       *fakeMetrics
}

func newWebhookHarness(t *testing.T) webhookHarness {
	t.Helper()
	accounts := newFakeAccounts()
	account := accounts.put(connectedAccount())
	svc := newFakeServices()
	svc.download = ttdomain.DownloadedMedia{Data: []byte("jpeg"), MimeType: "image/jpeg"}
	metrics := &fakeMetrics{}
	tokens := NewTokenSource(accounts, svc, fakeClassifier{}, &fakeAlerts{}, metrics)
	tokens.now = fixedClock
	w := webhookHarness{
		account: account, accounts: accounts, contacts: newFakeContacts(), conversations: newFakeConversations(), comments: newFakeComments(),
		svc: svc, history: &fakeHistory{}, media: &fakeMedia{}, watermarks: &fakeWatermarks{}, messages: &fakeMessageStore{messages: map[string]*conversation.Message{}},
		assignments: &fakeAssignments{}, dispatcher: &fakeDispatcher{}, sink: &fakeCommentSink{}, metrics: metrics,
	}
	w.h = NewHandleWebhook(HandleWebhookDeps{
		Repos:       Repositories{Accounts: accounts, Contacts: w.contacts, Conversations: w.conversations, Comments: w.comments},
		Tokens:      tokens,
		Messaging:   svc,
		Errors:      fakeClassifier{},
		History:     w.history,
		Messages:    w.messages,
		Announcer:   fakeAnnouncer{},
		Watermarks:  w.watermarks,
		Media:       w.media,
		Images:      NewImageStore(&fakeFetcher{}, &fakeObjects{}),
		Assignments: w.assignments,
		Automation:  w.dispatcher,
		Comments:    w.sink,
		Side:        NewSideMessages(newFakeSharedState()),
		Metrics:     metrics,
	})
	w.h.now = fixedClock
	return w
}

func eventFromFixture(t *testing.T, name string) ttdomain.Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "domain", "tiktok", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	env, err := ttdomain.DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := ttdomain.Normalize(env)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestAReceivedTextOpensTheConversationRecordsItAndRunsAutomation(t *testing.T) {
	w := newWebhookHarness(t)
	w.account.AgentID = ptrStr("agent-1")
	w.account.EnableAgentResponses = true
	w.accounts.put(w.account)
	ev := eventFromFixture(t, "receive_text.json")
	if err := w.h.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	records := w.history.all()
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	r := records[0]
	if r.EntryType != shared.EntryTypeTikTok || r.Channel != conversation.MessageChannelTikTok || r.MessageType != conversation.MessageTypeUserMessage || r.Text != "Oi, vocês entregam em Campinas?" || r.ProviderMessageID != "7381230000000000001" {
		t.Fatalf("record = %+v", r)
	}
	if r.SentBy != conversation.SentByContact(ev.Message.PersonID) {
		t.Errorf("sent by = %+v", r.SentBy)
	}
	conv, _ := w.conversations.FindByConversationID(context.Background(), w.account.ID, "S:1:0:abc+def/ghi=")
	if conv == nil || conv.LastCustomerMessageAt == nil || !conv.Window(ev.Message.SentAt.Add(time.Minute)).Open {
		t.Fatalf("conversation window not opened: %+v", conv)
	}
	if len(w.assignments.calls) != 1 || !strings.HasSuffix(w.assignments.calls[0], ":tiktok:"+w.account.ID) {
		t.Errorf("assignment = %v", w.assignments.calls)
	}
	if len(w.dispatcher.inputs) != 1 {
		t.Fatalf("automation not dispatched")
	}
	in := w.dispatcher.inputs[0]
	if in.EntryType != shared.EntryTypeTikTok || in.Text != r.Text || in.ContactRef != ev.Message.PersonID || !in.Config.EnableAgentResponses || in.Selection != nil {
		t.Errorf("automation input = %+v", in)
	}
}

func TestAButtonReplyDispatchesTheSelection(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "receive_button_reply.json")); err != nil {
		t.Fatal(err)
	}
	sel := w.dispatcher.inputs[0].Selection
	if sel == nil || sel.ID != "size_m" || sel.Title != "Quero o tamanho M" {
		t.Fatalf("selection = %+v", sel)
	}
}

func TestAReceivedImageIsStoredAsMedia(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "receive_image.json")); err != nil {
		t.Fatal(err)
	}
	r := w.history.all()[0]
	if r.MessageType != conversation.MessageTypeMedia || r.MediaType != conversation.MediaTypeImage || r.MediaURL == "" || len(w.media.stored) != 1 {
		t.Fatalf("record = %+v media %+v", r, w.media.stored)
	}
	if !strings.HasPrefix(w.media.stored[0].Key, "tiktok/") || string(w.media.stored[0].Data) != "jpeg" {
		t.Errorf("stored = %+v", w.media.stored[0])
	}
}

func TestAMediaDownloadFailureKeepsTheMessageWithAPlaceholder(t *testing.T) {
	w := newWebhookHarness(t)
	w.svc.downloadErr = codedError{retry: true}
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "receive_image.json")); err != nil {
		t.Fatal(err)
	}
	r := w.history.all()[0]
	if r.MessageType != conversation.MessageTypeUnsupported || r.Text != PlaceholderMedia {
		t.Fatalf("record = %+v", r)
	}
	if w.metrics.count("media:download_failed") != 1 {
		t.Errorf("metrics = %v", w.metrics.events)
	}
}

func TestAnEchoFromTheTikTokAppIsRecordedCountedAndNeverAutomated(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "receive_text.json")); err != nil {
		t.Fatal(err)
	}
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "send_from_app.json")); err != nil {
		t.Fatal(err)
	}
	records := w.history.all()
	echo := records[len(records)-1]
	if echo.MessageType != conversation.MessageTypeOperator || echo.SentBy != conversation.SentExternally() || echo.Text != "Entregamos sim!" {
		t.Fatalf("echo = %+v", echo)
	}
	var meta map[string]any
	_ = json.Unmarshal(echo.Metadata, &meta)
	if meta[metadataSentVia] != "tiktok_app" {
		t.Errorf("metadata = %v", meta)
	}
	if w.conversations.counted != 1 {
		t.Errorf("an echo from the app must spend the budget, counted %d", w.conversations.counted)
	}
	if len(w.dispatcher.inputs) != 1 {
		t.Errorf("echoes must not run automation, dispatched %d", len(w.dispatcher.inputs))
	}
}

func TestAnEchoOfOurOwnSendDoesNotSpendTheBudgetTwice(t *testing.T) {
	w := newWebhookHarness(t)
	ev := eventFromFixture(t, "send_from_app.json")
	ev.Message.Source = ttdomain.SourceAPI
	if err := w.h.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if w.conversations.counted != 0 {
		t.Fatal("our own sends already claimed the budget")
	}
}

func TestATikTokAutoMessageEchoIsMarkedAsTikTokAutomation(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "send_auto_message.json")); err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	_ = json.Unmarshal(w.history.all()[0].Metadata, &meta)
	if meta[metadataSentVia] != "tiktok_automation" || meta[metadataAutoMessage] != "WELCOME_MESSAGE" {
		t.Fatalf("metadata = %v", meta)
	}
}

func TestEventsForUnknownAccountsAreRefusedWithoutWritingAnything(t *testing.T) {
	w := newWebhookHarness(t)
	ev := eventFromFixture(t, "receive_text.json")
	ev.AccountOpenID = "_000unknown"
	if err := w.h.Handle(context.Background(), ev); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("err = %v", err)
	}
	if len(w.history.all()) != 0 || len(w.contacts.byID) != 0 {
		t.Fatal("nothing must be written for an unknown account")
	}
}

func TestMessagesForAnAccountWithoutMessagingAreRefused(t *testing.T) {
	w := newWebhookHarness(t)
	w.account.Capabilities.CanReceiveMessages = false
	w.accounts.put(w.account)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "receive_text.json")); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if len(w.history.all()) != 0 {
		t.Fatal("nothing must be recorded")
	}
}

func TestAReadAdvancesTheOutboundWatermark(t *testing.T) {
	w := newWebhookHarness(t)
	_ = w.h.Handle(context.Background(), eventFromFixture(t, "receive_text.json"))
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "mark_read.json")); err != nil {
		t.Fatal(err)
	}
	if len(w.watermarks.calls) != 1 || !w.watermarks.calls[0].Equal(time.UnixMilli(1701090890815).UTC()) {
		t.Fatalf("watermarks = %v", w.watermarks.calls)
	}
}

func TestAReferralIsStoredOnTheConversation(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "referral_ad.json")); err != nil {
		t.Fatal(err)
	}
	conv, _ := w.conversations.FindByConversationID(context.Background(), w.account.ID, "S:1:0:abc+def/ghi=")
	if conv.Referral == nil || conv.Referral.AdID != "1800000000000000001" {
		t.Fatalf("conversation = %+v", conv)
	}
	if conv.LastCustomerMessageAt != nil {
		t.Error("a referral is not a customer message and must not open the window")
	}
}

func TestAReactionUpdatesTheOriginalMessage(t *testing.T) {
	w := newWebhookHarness(t)
	_ = w.h.Handle(context.Background(), eventFromFixture(t, "receive_text.json"))
	conv, _ := w.conversations.FindByConversationID(context.Background(), w.account.ID, "S:1:0:abc+def/ghi=")
	w.messages.messages[conv.ID+":out-1"] = &conversation.Message{ID: "m-1", EntryID: conv.ID}
	ev := eventFromFixture(t, "receive_text.json")
	ev.Message.Type = ttdomain.MessageReaction
	ev.Message.MessageID = "react-1"
	ev.Message.Reactions = []ttdomain.Reaction{{Operation: "ADD", Type: "EMOJI", Emoji: "❤️", OriginalMessageID: "out-1", ReactedAt: testNow}}
	if err := w.h.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if w.messages.updated != 1 {
		t.Fatalf("reaction not saved")
	}
	if len(w.dispatcher.inputs) != 1 {
		t.Error("a reaction must not run automation")
	}
}

func TestAnEUMessageIsCountedAndWritesNothing(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "receive_eu.json")); err != nil {
		t.Fatal(err)
	}
	if len(w.history.all()) != 0 || w.metrics.count("message:eu_without_content") != 1 {
		t.Fatalf("history %v metrics %v", w.history.all(), w.metrics.events)
	}
}

func TestACommentInsertLinksTheCommenterAndQueuesItsWork(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "comment_insert.json")); err != nil {
		t.Fatal(err)
	}
	if len(w.comments.writes) != 1 {
		t.Fatalf("writes = %+v", w.comments.writes)
	}
	write := w.comments.writes[0]
	if write.ContactID == nil || write.KnownOurs || write.Event.CommentID != "7247303576418566913" {
		t.Fatalf("write = %+v", write)
	}
	contact, err := w.contacts.FindByID(context.Background(), *write.ContactID)
	if err != nil || contact.UniqueIdentifier != write.Event.PersonID {
		t.Fatalf("contact %+v err %v", contact, err)
	}
}

func TestACommentFromTheAccountItselfIsKnownToBeOurs(t *testing.T) {
	w := newWebhookHarness(t)
	ev := eventFromFixture(t, "comment_insert.json")
	w.account.UniqueIdentifier = ev.Comment.PersonID
	w.accounts.put(w.account)
	if err := w.h.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if !w.comments.writes[0].KnownOurs {
		t.Fatal("the account's own comment must be marked as ours")
	}
}

func TestADeletedCommentIsForgottenByTheAudience(t *testing.T) {
	w := newWebhookHarness(t)
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "comment_reply_delete.json")); err != nil {
		t.Fatal(err)
	}
	if len(w.sink.forgotten) != 1 || w.sink.forgotten[0] != "7247303576418566914" {
		t.Fatalf("forgotten = %v", w.sink.forgotten)
	}
}

func TestAStaleCommentEventChangesNothingDownstream(t *testing.T) {
	w := newWebhookHarness(t)
	w.comments.applied = false
	if err := w.h.Handle(context.Background(), eventFromFixture(t, "comment_reply_delete.json")); err != nil {
		t.Fatal(err)
	}
	if len(w.sink.forgotten) != 0 {
		t.Fatal("a stale delete must not reach the audience")
	}
}
