package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

const (
	anaChat   = "5511111111111@s.whatsapp.net"
	brunoChat = "5511222222222@s.whatsapp.net"
	groupChat = "120363000000000000@g.us"
)

type storedMessages struct {
	conversation.MessageRepository
	mu       sync.Mutex
	existing map[string]bool
	lookups  int
	err      error
}

func (s *storedMessages) GetByEntryAndExternalMessageID(_ shared.EntryType, entryID, providerID string) (*conversation.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups++
	if s.err != nil {
		return nil, s.err
	}
	if s.existing[entryID+":"+providerID] {
		return &conversation.Message{ID: "stored-" + providerID}, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (s *storedMessages) remember(entryID, providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.existing[entryID+":"+providerID] = true
}

type entryUpdates struct {
	conversation.EventBroadcaster
	mu      sync.Mutex
	entries []string
}

func (b *entryUpdates) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, entryID)
}

func (b *entryUpdates) all() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.entries...)
}

type countingDownloads struct {
	*fakeMessaging
	n int32
}

func (c *countingDownloads) DownloadMedia(ctx context.Context, ref uw.InstanceRef, id string) (*uw.RemoteMedia, error) {
	atomic.AddInt32(&c.n, 1)
	return c.fakeMessaging.DownloadMedia(ctx, ref, id)
}

type failingHistory struct{ err error }

func (f failingHistory) Record(context.Context, conversation.MessageHistoryRecord) error {
	return f.err
}

type importHarness struct {
	uc          *HandleWebhookUseCase
	instance    *uw.Instance
	history     *recordingHistory
	messages    *storedMessages
	broadcasts  *entryUpdates
	assignments *recordingAssignments
	workflows   *recordingWorkflows
	ai          *recordingAI
	analysis    *enrichmentRecorder
	leads       *fakeLeadLinker
	downloads   *countingDownloads
	convs       *fakeConversationRepo
}

func newImportHarness(t *testing.T) *importHarness {
	t.Helper()
	agentID := "agent-1"
	workflowID := "wf-1"
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: uw.StatusConnected, PhoneNumber: "5599999999999",
		AgentID: &agentID, EnableAgentResponses: true,
		EnableWorkflow: true, WorkflowID: &workflowID,
		EnableAnalysis: true,
		ImportHistory:  true,
	}
	h := &importHarness{
		instance:    instance,
		history:     &recordingHistory{},
		messages:    &storedMessages{existing: map[string]bool{}},
		broadcasts:  &entryUpdates{},
		assignments: &recordingAssignments{},
		workflows:   &recordingWorkflows{},
		ai:          &recordingAI{},
		analysis:    &enrichmentRecorder{},
		leads:       newFakeLeadLinker(),
		downloads:   &countingDownloads{fakeMessaging: &fakeMessaging{}},
		convs:       newFakeConversationRepo(),
	}
	h.uc = NewHandleWebhookUseCase(HandleWebhookDeps{
		Instances:     newFakeInstanceRepo(instance),
		Servers:       newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		Contacts:      newFakeContactRepo(),
		Conversations: h.convs,
		Groups:        newFakeGroupRepo(),
		Messaging:     h.downloads,
		GroupAPI:      &fakeGroupAPI{},
		Assets:        &fakeAssets{},
		FileStorage:   newFakeStorage(),
		ConvMedia:     &fakeConvMedia{},
		History:       h.history,
		Messages:      h.messages,
		Broadcaster:   h.broadcasts,
		Assignments:   h.assignments,
		AIReply:       h.ai,
		Workflows:     h.workflows,
		Leads:         h.leads,
		Analysis:      h.analysis,
	})
	return h
}

func historyItem(id, chat string, fromMe bool, at time.Time) map[string]any {
	item := map[string]any{
		"messageid":        id,
		"chatid":           chat,
		"fromMe":           fromMe,
		"text":             "mensagem " + id,
		"messageTimestamp": at.UnixMilli(),
	}
	if !fromMe {
		item["sender"] = chat
		item["senderName"] = "Contato " + id
	}
	return item
}

func (h *importHarness) deliverBatch(t *testing.T, items ...map[string]any) error {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"EventType": "history", "batchNumber": 1, "batchTotal": 1, "messages": items,
	})
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	return h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: h.instance.ID, Body: body})
}

func (h *importHarness) importPage(t *testing.T, windowFrom time.Time, items ...map[string]any) historyImport {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("encode page: %v", err)
	}
	return h.uc.importHistory(context.Background(), h.instance, uw.HistoryEnvelope(raw), windowFrom)
}

func TestHistoryBatchLandsReadAndSilentWithoutAnyAutomation(t *testing.T) {
	h := newImportHarness(t)
	now := time.Now().UTC()

	err := h.deliverBatch(t,
		historyItem("a1", anaChat, false, now.Add(-2*time.Hour)),
		historyItem("a2", anaChat, true, now.Add(-time.Hour)),
		historyItem("b1", brunoChat, false, now.Add(-30*time.Minute)),
	)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	records := h.history.all()
	if len(records) != 3 {
		t.Fatalf("recorded %d messages, want 3", len(records))
	}
	for _, r := range records {
		if !r.Read || !r.Silent {
			t.Errorf("message %s: read=%v silent=%v; an import must not raise unread badges or per-message frames",
				r.ProviderMessageID, r.Read, r.Silent)
		}
	}
	if n := h.assignments.count(); n != 0 {
		t.Errorf("assignment ran %d times; imported history must not trigger the roulette", n)
	}
	if n := len(h.workflows.all()); n != 0 {
		t.Errorf("workflows fired %d times on history", n)
	}
	if n := len(h.ai.all()); n != 0 {
		t.Errorf("the AI replied %d times to history", n)
	}
	if h.analysis.calls != 0 {
		t.Errorf("analysis scheduled %d times for history", h.analysis.calls)
	}
	if got := h.broadcasts.all(); len(got) != 2 {
		t.Errorf("entry updates = %v, want exactly one per conversation", got)
	}
}

func TestHistoryAlreadyStoredIsADuplicateAndSkipsTheMediaDownload(t *testing.T) {
	h := newImportHarness(t)
	now := time.Now().UTC()
	photo := historyItem("p1", anaChat, false, now.Add(-time.Hour))
	photo["messageType"] = "image"
	photo["mimetype"] = "image/jpeg"

	first := h.importPage(t, time.Time{}, photo)
	conv, err := h.convs.FindByChatID(context.Background(), h.instance.ID, anaChat)
	if err != nil {
		t.Fatalf("conversation not created: %v", err)
	}
	h.messages.remember(conv.ID, "p1")
	second := h.importPage(t, time.Time{}, photo)

	if first.pass.Imported != 1 || second.pass.Duplicate != 1 || second.pass.Imported != 0 {
		t.Errorf("first=%+v second=%+v", first.pass, second.pass)
	}
	if n := atomic.LoadInt32(&h.downloads.n); n != 1 {
		t.Errorf("media downloaded %d times; a re-poll must not re-download what is stored", n)
	}
	if n := len(h.history.all()); n != 1 {
		t.Errorf("recorded %d times, want 1", n)
	}
}

func TestHistoryDuplicateCheckFailureDoesNotStoreBlind(t *testing.T) {
	h := newImportHarness(t)
	h.messages.err = errors.New("db down")

	result := h.importPage(t, time.Time{}, historyItem("a1", anaChat, false, time.Now().Add(-time.Hour)))

	if result.pass.Failed != 1 || result.pass.Imported != 0 {
		t.Errorf("pass = %+v; an unanswerable duplicate check must fail the message, not store it", result.pass)
	}
	if len(h.history.all()) != 0 {
		t.Error("a message was stored without knowing whether it already existed")
	}
}

func TestHistoryOlderThanTheWindowIsSkippedAndStopsPaging(t *testing.T) {
	h := newImportHarness(t)
	now := time.Now().UTC()
	windowFrom := now.Add(-24 * time.Hour)

	result := h.importPage(t, windowFrom,
		historyItem("new", anaChat, false, now.Add(-time.Hour)),
		historyItem("old", anaChat, false, now.Add(-48*time.Hour)),
	)

	if result.pass.Imported != 1 || result.pass.Skipped != 1 {
		t.Errorf("pass = %+v", result.pass)
	}
	if !result.reachedWindow {
		t.Error("a message older than the window must tell the poller to stop paging")
	}
}

func TestHistoryGroupsAreSkippedUnlessTheNumberHandlesGroups(t *testing.T) {
	h := newImportHarness(t)
	item := historyItem("g1", groupChat, false, time.Now().Add(-time.Hour))
	item["sender"] = "5511333333333@s.whatsapp.net"
	item["isGroup"] = true

	skipped := h.importPage(t, time.Time{}, item)
	h.instance.HandleGroups = true
	imported := h.importPage(t, time.Time{}, item)

	if skipped.pass.Skipped != 1 || skipped.pass.Imported != 0 {
		t.Errorf("groups off: pass = %+v", skipped.pass)
	}
	if imported.pass.Imported != 1 {
		t.Errorf("groups on: pass = %+v", imported.pass)
	}
}

func TestHistoryBridgesALeadOnlyForChatsTheContactWroteIn(t *testing.T) {
	h := newImportHarness(t)
	now := time.Now().UTC()

	h.importPage(t, time.Time{},
		historyItem("a1", anaChat, false, now.Add(-time.Hour)),
		historyItem("b1", brunoChat, true, now.Add(-time.Hour)),
	)

	h.leads.mu.Lock()
	defer h.leads.mu.Unlock()
	if _, ok := h.leads.byPhone["5511111111111"]; !ok {
		t.Errorf("no lead for the contact who wrote in: %v", h.leads.byPhone)
	}
	if _, ok := h.leads.byPhone["5511222222222"]; ok {
		t.Error("a lead was created for a number the business only wrote to; that is the address book, not a lead")
	}
}

func TestHistoryIgnoresNonMessageEvents(t *testing.T) {
	h := newImportHarness(t)
	reaction := historyItem("r1", anaChat, false, time.Now().Add(-time.Hour))
	reaction["messageType"] = "reaction"
	reaction["reaction"] = "a1"

	result := h.importPage(t, time.Time{}, reaction)

	if result.pass.Skipped != 1 || len(h.history.all()) != 0 {
		t.Errorf("pass = %+v", result.pass)
	}
}

func TestHistoryStoreFailureFailsTheBatchSoTheQueueRetries(t *testing.T) {
	h := newImportHarness(t)
	h.uc.history = failingHistory{err: errors.New("insert failed")}

	err := h.deliverBatch(t, historyItem("a1", anaChat, false, time.Now().Add(-time.Hour)))

	if err == nil {
		t.Error("a batch whose messages failed to save must return an error so it is retried")
	}
}

func TestHistoryBatchWithOnlyChatsIsANoOp(t *testing.T) {
	h := newImportHarness(t)
	body := []byte(`{"EventType":"history","batchNumber":1,"batchTotal":3,"chats":[{"wa_chatid":"5511@s.whatsapp.net"}]}`)

	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: h.instance.ID, Body: body}); err != nil {
		t.Fatalf("chats batch failed: %v", err)
	}
	if len(h.history.all()) != 0 {
		t.Error("a chats-only batch must not record anything")
	}
}

func TestHistoryBatchBeforeTheConnectIsRecordedIsRetried(t *testing.T) {
	h := newImportHarness(t)
	h.instance.Status = uw.StatusAwaitingScan

	err := h.deliverBatch(t, historyItem("a1", anaChat, false, time.Now().Add(-time.Hour)))

	if !errors.Is(err, errHistoryBeforeConnected) {
		t.Errorf("err = %v; the batch must wait for the handover rather than open duplicate threads", err)
	}
	if len(h.history.all()) != 0 {
		t.Error("a batch was imported before the number was recorded as connected")
	}
}

func TestHistoryBatchHandsTheLineOverFirst(t *testing.T) {
	h := newImportHarness(t)
	lines := &fakeLines{siblings: []*uw.Instance{{
		ID: "old-inst", WorkspaceID: "ws-1", PhoneNumber: h.instance.PhoneNumber, Status: uw.StatusDisconnected,
	}}}
	h.uc.SetLineHandover(NewLineHandover(lines))

	if err := h.deliverBatch(t, historyItem("a1", anaChat, false, time.Now().Add(-time.Hour))); err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(lines.transfers) != 1 {
		t.Errorf("transfers = %v", lines.transfers)
	}
}

func TestHistoryBatchIsDroppedWhenTheImportIsSwitchedOff(t *testing.T) {
	h := newImportHarness(t)
	h.uc.DisableHistoryImport()

	if err := h.deliverBatch(t, historyItem("a1", anaChat, false, time.Now().Add(-time.Hour))); err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(h.history.all()) != 0 {
		t.Error("the kill switch must stop webhook history too")
	}
}

func TestHistoryBatchesCarryNoEventDedupKeySoRetriesReallyRun(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"EventType": "history", "messages": []map[string]any{historyItem("a1", anaChat, false, time.Now())},
	})
	if err != nil {
		t.Fatal(err)
	}

	if key := dedupKeyForEvent(&QueuedEvent{InstanceID: "inst-1", Body: body}); key != "" {
		t.Errorf("key = %q; the durable claim is never released, so a keyed batch that fails once is dropped on retry", key)
	}
}

func TestHistoryAlreadyStoredInAnotherOfTheContactsConversationsIsADuplicate(t *testing.T) {
	h := newImportHarness(t)
	lidChat := "122630107566238@lid"
	contactID := "contact-" + lidChat
	h.convs.convs["campaign-thread"] = &uw.Conversation{
		ID: "campaign-thread", InstanceID: h.instance.ID, ContactID: contactID,
		ChatID: anaChat, CampaignID: "camp-1",
	}
	h.messages.remember("campaign-thread", "sent-in-campaign")
	h.convs.convs["walk-in"] = &uw.Conversation{
		ID: "walk-in", InstanceID: h.instance.ID, ContactID: contactID, ChatID: lidChat,
	}

	result := h.importPage(t, time.Time{}, historyItem("sent-in-campaign", lidChat, true, time.Now().Add(-time.Hour)))

	if result.pass.Duplicate != 1 || len(h.history.all()) != 0 {
		t.Errorf("pass = %+v; a message the campaign thread already holds was copied into another thread", result.pass)
	}
}

func TestHistoryRePollHealsAStaleProfileWithoutReadingItPerMessage(t *testing.T) {
	h := newImportHarness(t)
	gate := newProfileGate()
	h.uc.profiles.gate = gate
	h.uc.groups.profiles.gate = gate
	item := historyItem("a1", anaChat, false, time.Now().Add(-time.Hour))

	h.importPage(t, time.Time{}, item)
	conv, err := h.convs.FindByChatID(context.Background(), h.instance.ID, anaChat)
	if err != nil {
		t.Fatal(err)
	}
	h.messages.remember(conv.ID, "a1")
	h.importPage(t, time.Time{}, item, item)

	if got := len(h.downloads.chatDetailCalls()); got != 1 {
		t.Errorf("profile reads = %d; one read per contact, and a fresh profile is not read again", got)
	}
}

func TestHistoryBatchHonoursTheImportWindow(t *testing.T) {
	h := newImportHarness(t)
	h.uc.SetHistoryWindow(30 * 24 * time.Hour)

	err := h.deliverBatch(t,
		historyItem("recent", anaChat, false, time.Now().Add(-24*time.Hour)),
		historyItem("ancient", anaChat, false, time.Now().Add(-60*24*time.Hour)),
	)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}

	records := h.history.all()
	if len(records) != 1 || records[0].ProviderMessageID != "recent" {
		t.Errorf("recorded %d messages; the webhook must honour the same window as the poller", len(records))
	}
}

func TestHistoryBatchIsDroppedWhenTheNumberOptedOut(t *testing.T) {
	h := newImportHarness(t)
	h.instance.ImportHistory = false

	if err := h.deliverBatch(t, historyItem("a1", anaChat, false, time.Now().Add(-time.Hour))); err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(h.history.all()) != 0 {
		t.Error("history was imported for a number whose owner chose not to restore it")
	}
}
