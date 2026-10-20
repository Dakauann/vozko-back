package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

const (
	sellerID = "415458330"
	buyerID  = "123456789"
	packKey  = "2000000089077943"
	agentID  = "3037675074"
)

type handlerFixture struct {
	dispatcher    *NotificationDispatcher
	accounts      *fakeAccounts
	contacts      *fakeContacts
	conversations *fakeConversations
	messages      *fakeMessages
	attachments   *fakeAttachments
	sales         *fakeSales
	users         *fakeUsers
	history       *fakeHistory
	store         *fakeMessageStore
	media         *fakeMedia
	watermarks    *fakeWatermarks
	assignments   *fakeAssignments
	automation    *fakeDispatcher
	metrics       *fakeMetrics
	alerts        *fakeAlerts
	oauth         *fakeOAuth
	claims        *fakeInboundClaims
}

func newHandlerFixture() *handlerFixture {
	f := &handlerFixture{
		accounts:      newFakeAccounts(),
		contacts:      newFakeContacts(),
		conversations: newFakeConversations(),
		messages:      &fakeMessages{threads: map[string]*mldomain.Thread{}, messages: map[string]*mldomain.RemoteMessage{}},
		attachments:   &fakeAttachments{files: map[string]*mldomain.AttachmentDownload{}},
		sales: &fakeSales{
			packs:     map[string]*mldomain.RemotePack{packKey: {ID: packKey, BuyerID: buyerID, ShipmentID: "40000000001", OrderIDs: []string{"2000000000000001"}}},
			orders:    map[string]*mldomain.RemoteOrder{"2000000000000001": {ID: "2000000000000001", PackID: packKey, Status: "paid", BuyerID: buyerID, TotalAmount: 99.9, CurrencyID: "BRL", Items: []mldomain.RemoteOrderItem{{ItemID: "MLB1", Title: "Caneca", Quantity: 1, UnitPrice: 99.9}}}},
			shipments: map[string]*mldomain.RemoteShipment{"40000000001": {ID: "40000000001", Status: "shipped"}},
		},
		users:       &fakeUsers{users: map[string]*mldomain.User{buyerID: {ID: buyerID, Nickname: "COMPRADOR"}}},
		history:     &fakeHistory{},
		store:       newFakeMessageStore(),
		media:       &fakeMedia{},
		watermarks:  &fakeWatermarks{},
		assignments: &fakeAssignments{},
		automation:  &fakeDispatcher{},
		metrics:     newFakeMetrics(),
		alerts:      &fakeAlerts{},
		oauth:       &fakeOAuth{refresh: rotated},
		claims:      &fakeInboundClaims{},
	}
	f.accounts.put(connectedAccount())
	tokens := NewTokenSource(f.accounts, f.oauth, fakeClassifier{}, f.alerts, f.metrics)
	tokens.now = fixedClock
	svc := Services{
		Users: f.users, Messages: f.messages, Attachments: f.attachments,
		Packs: f.sales, Orders: f.sales, Shipments: f.sales, Errors: fakeClassifier{},
	}
	svc.Items = &fakeItems{items: map[string]*mldomain.RemoteItem{}}
	summaries := NewPackSummaries(PackSummariesDeps{Conversations: f.conversations, Listings: newFakeListings(), Services: svc, Images: &fakeImages{}, Metrics: f.metrics})
	summaries.now = fixedClock
	threads := NewThreadSync(ThreadSyncDeps{
		Contacts: f.contacts, Conversations: f.conversations, Services: svc, Tokens: tokens,
		History: f.history, Messages: f.store, Media: f.media, Watermarks: f.watermarks,
		Assignments: f.assignments, Automation: f.automation, Dispatches: f.claims, Announcer: &fakeBroadcaster{},
		PackSummaries: summaries, Metrics: f.metrics,
	})
	threads.now = fixedClock
	f.dispatcher = NewNotificationDispatcher(f.accounts, f.metrics, map[mldomain.JobKind]JobHandler{
		mldomain.JobFetchMessage:  threads,
		mldomain.JobFetchThread:   threads,
		mldomain.JobFetchOrder:    NewSalesSync(f.conversations, svc, tokens, summaries, threads),
		mldomain.JobFetchShipment: NewSalesSync(f.conversations, svc, tokens, summaries, threads),
		mldomain.JobRefreshPack:   NewSalesSync(f.conversations, svc, tokens, summaries, threads),
	})
	return f
}

func activeThread(path string, msgs ...*mldomain.RemoteMessage) *mldomain.Thread {
	return &mldomain.Thread{Status: mldomain.ThreadStatusActive, Path: path, Messages: msgs}
}

func directPath() string { return "/packs/" + packKey + "/sellers/" + sellerID }

func mediatedPath() string { return directPath() + "/conversations/abc123" }

func buyerMessage(id, text string, at time.Time) *mldomain.RemoteMessage {
	return &mldomain.RemoteMessage{ID: id, FromUserID: buyerID, ToUserID: sellerID, Text: text, Status: mldomain.MessageAvailable, CreatedAt: at, PackKey: packKey}
}

func messageJob(id string) *mldomain.Job {
	return &mldomain.Job{Kind: mldomain.JobFetchMessage, Topic: "messages", SellerID: sellerID, ResourceID: id, Resource: id, NotificationID: "n-" + id}
}

func TestABuyerMessageReachesTheCRMAndTheAutomation(t *testing.T) {
	f := newHandlerFixture()
	msg := buyerMessage("0123456789abcdef0001", "Quando chega?", testNow.Add(-time.Minute))
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)

	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(f.conversations.rows) != 1 {
		t.Fatalf("conversations = %d", len(f.conversations.rows))
	}
	var conv *mldomain.Conversation
	for _, c := range f.conversations.rows {
		conv = c
	}
	if conv.BuyerMLUserID != buyerID || conv.MLStatus != "active" || !conv.WindowOpen || conv.AgentMediated == nil || *conv.AgentMediated {
		t.Fatalf("conversation = %+v", conv)
	}
	if conv.PackSummary == nil || conv.PackSummary.OrderStatus != "paid" || conv.PackSummary.ShippingStatus != "shipped" || len(conv.PackSummary.Items) != 1 {
		t.Fatalf("pack summary = %+v", conv.PackSummary)
	}
	if len(f.history.records) != 1 {
		t.Fatalf("records = %d", len(f.history.records))
	}
	r := f.history.records[0]
	if r.EntryType != shared.EntryTypeMercadoLivre || r.Channel != conversation.MessageChannelMercadoLivre || r.ProviderMessageID != msg.ID ||
		!r.SentBy.IsContact() || r.Text != "Quando chega?" || r.SenderName != "COMPRADOR" {
		t.Fatalf("record = %+v", r)
	}
	if len(f.conversations.inbound) != 1 || len(f.assignments.calls) != 1 || !strings.HasSuffix(f.assignments.calls[0], "|mercadolivre|acct-1") {
		t.Fatalf("inbound=%v assignments=%v", f.conversations.inbound, f.assignments.calls)
	}
	if len(f.automation.inputs) != 1 {
		t.Fatalf("automation = %d", len(f.automation.inputs))
	}
	in := f.automation.inputs[0]
	if in.EntryType != shared.EntryTypeMercadoLivre || in.Text != "Quando chega?" || in.ProviderMessageID != msg.ID || in.Config.RunsAnalysis() {
		t.Fatalf("automation input = %+v", in)
	}
	for _, ctx := range f.messages.ctxs {
		if mldomain.IsBackground(ctx) {
			t.Fatal("a live notification must not spend the background share")
		}
	}
}

func TestARedeliveredMessageRunsNothingTwice(t *testing.T) {
	f := newHandlerFixture()
	msg := buyerMessage("0123456789abcdef0001", "oi", testNow)
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	var convID string
	for id := range f.conversations.rows {
		convID = id
	}
	ext := msg.ID
	f.store.byExt[convID+"|"+ext] = &conversation.Message{ID: "m-1", EntryID: convID, ExternalMessageID: &ext, SentBy: conversation.SentByContact(buyerID)}
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 1 || len(f.automation.inputs) != 1 || len(f.conversations.inbound) != 1 {
		t.Fatalf("records=%d automation=%d inbound=%d", len(f.history.records), len(f.automation.inputs), len(f.conversations.inbound))
	}
}

func TestNotificationsRacingOnOneNewMessageDispatchAutomationOnce(t *testing.T) {
	f := newHandlerFixture()
	msg := buyerMessage("0123456789abcdef0001", "Tem garantia?", testNow)
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	for _, notificationID := range []string{"n-created", "n-read", "n-retry"} {
		job := messageJob(msg.ID)
		job.NotificationID = notificationID
		if err := f.dispatcher.Execute(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.automation.inputs) != 1 {
		t.Fatalf("one buyer message ran the automation %d times", len(f.automation.inputs))
	}
	if f.metrics.count("consume/dispatch_duplicate") != 2 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func TestADispatchClaimFailureRunsNoAutomation(t *testing.T) {
	f := newHandlerFixture()
	f.claims.err = errors.New("database down")
	msg := buyerMessage("0123456789abcdef0001", "Tem garantia?", testNow)
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	if len(f.automation.inputs) != 0 || len(f.history.records) != 1 || f.metrics.count("consume/dispatch_claim_failed") != 1 {
		t.Fatalf("automation=%d records=%d metrics=%v", len(f.automation.inputs), len(f.history.records), f.metrics.events)
	}
}

func TestAnAgentMediatedThreadMirrorsMediationAndBudget(t *testing.T) {
	f := newHandlerFixture()
	msg := buyerMessage("0123456789abcdef0002", "Olá, vendedor", testNow)
	msg.FromUserID = agentID
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(mediatedPath(), msg)
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	var conv *mldomain.Conversation
	for _, c := range f.conversations.rows {
		conv = c
	}
	if conv.AgentMediated == nil || !*conv.AgentMediated || conv.OutboundBudget != 1 || conv.BuyerMLUserID != buyerID {
		t.Fatalf("conversation = %+v", conv)
	}
	var meta map[string]any
	_ = json.Unmarshal(f.history.records[0].Metadata, &meta)
	if meta["mercadolivre_from_agent"] != true {
		t.Fatalf("metadata = %s", f.history.records[0].Metadata)
	}
}

func TestAnUnknownSubstatusClosesTheWindow(t *testing.T) {
	f := newHandlerFixture()
	msg := buyerMessage("0123456789abcdef0003", "oi", testNow)
	f.messages.messages[msg.ID] = msg
	th := activeThread(directPath(), msg)
	th.Status, th.Substatus = mldomain.ThreadStatusBlocked, "blocked_by_something_new"
	f.messages.threads[packKey] = th
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.conversations.rows {
		if c.WindowOpen || c.MLSubstatus != "blocked_by_something_new" {
			t.Fatalf("conversation = %+v", c)
		}
	}
	if f.metrics.count("thread/unknown_status") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func TestASellerMessageWeDidNotSendIsRecordedWithoutAutomation(t *testing.T) {
	f := newHandlerFixture()
	msg := &mldomain.RemoteMessage{ID: "0123456789abcdef0004", FromUserID: sellerID, ToUserID: buyerID, Text: "Enviado hoje", Status: mldomain.MessageAvailable, CreatedAt: testNow, PackKey: packKey}
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 1 || len(f.automation.inputs) != 0 || len(f.conversations.inbound) != 0 {
		t.Fatalf("records=%d automation=%d inbound=%d", len(f.history.records), len(f.automation.inputs), len(f.conversations.inbound))
	}
	r := f.history.records[0]
	if r.SentBy.Kind() != conversation.SenderExternal || r.MessageType != conversation.MessageTypeOperator || r.SenderName != "" {
		t.Fatalf("record = %+v", r)
	}
}

func TestOurOwnMessageReconcilesItsModeration(t *testing.T) {
	f := newHandlerFixture()
	msg := &mldomain.RemoteMessage{ID: "0123456789abcdef0005", FromUserID: sellerID, ToUserID: buyerID, Text: "Segue o link",
		Status: mldomain.MessageModerated, ModerationStatus: "rejected", ModerationReason: "SOCIAL_NETWORK_LINK", CreatedAt: testNow, PackKey: packKey}
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	contact, _ := f.contacts.FindOrCreateByUserID(context.Background(), mldomain.FindOrCreateContactInput{WorkspaceID: "ws-1", AccountID: "acct-1", MLUserID: buyerID})
	conv, _, _ := f.conversations.FindOrCreateByPack(context.Background(), mldomain.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", AccountID: "acct-1", ContactID: contact.ID, PackKey: packKey, BuyerMLUserID: buyerID,
	})
	ext := msg.ID
	f.store.byExt[conv.ID+"|"+ext] = &conversation.Message{ID: "m-own", EntryID: conv.ID, EntryType: shared.EntryTypeMercadoLivre,
		ExternalMessageID: &ext, SentBy: conversation.SentByPerson("u-1"), DeliveryStatus: conversation.DeliveryStatusSent}

	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 0 {
		t.Fatal("our own message must not be recorded twice")
	}
	if len(f.store.updated) != 1 {
		t.Fatalf("updates = %d", len(f.store.updated))
	}
	updated := f.store.updated[0]
	var meta map[string]map[string]string
	if err := json.Unmarshal(updated.Metadata, &meta); err != nil || meta[ModerationMetadataKey]["reason"] != "SOCIAL_NETWORK_LINK" {
		t.Fatalf("metadata = %s", updated.Metadata)
	}
	if updated.DeliveryStatus != conversation.DeliveryStatusFailed {
		t.Fatalf("a rejected message is failed: %q", updated.DeliveryStatus)
	}

	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	if len(f.store.updated) != 1 {
		t.Fatal("an unchanged moderation state is not written again")
	}
}

func TestAFailedAttachmentDownloadKeepsTheMessage(t *testing.T) {
	f := newHandlerFixture()
	msg := buyerMessage("0123456789abcdef0006", "", testNow)
	msg.Attachments = []mldomain.RemoteAttachment{
		{ID: "415460047_ok.png", OriginalName: "foto.png", ContentType: "image/png", Size: 3},
		{ID: "415460047_gone.pdf", OriginalName: "nota.pdf", ContentType: "application/pdf", Size: 3},
	}
	f.attachments.files["415460047_ok.png"] = &mldomain.AttachmentDownload{Data: []byte("png"), ContentType: "image/png"}
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	if err := f.dispatcher.Execute(context.Background(), messageJob(msg.ID)); err != nil {
		t.Fatal(err)
	}
	if len(f.media.stored) != 1 || len(f.history.records) != 2 {
		t.Fatalf("stored=%d records=%d", len(f.media.stored), len(f.history.records))
	}
	if f.history.records[0].MediaType != conversation.MediaTypeImage || f.history.records[0].ProviderMessageID != msg.ID+":att0" {
		t.Fatalf("media record = %+v", f.history.records[0])
	}
	missing := f.history.records[1]
	var meta map[string]any
	_ = json.Unmarshal(missing.Metadata, &meta)
	if missing.MessageType != conversation.MessageTypeUnsupported || meta[MediaUnavailableMetadataKey] != true || missing.ProviderMessageID != msg.ID+":att1" {
		t.Fatalf("missing media record = %+v (%s)", missing, missing.Metadata)
	}
	if len(f.automation.inputs) != 1 {
		t.Fatal("an attachment-only message still reaches the automation")
	}
}

func TestAReadActionMarksOutboundMessagesRead(t *testing.T) {
	f := newHandlerFixture()
	readAt := testNow.Add(-time.Minute)
	msg := &mldomain.RemoteMessage{ID: "0123456789abcdef0007", FromUserID: sellerID, ToUserID: buyerID, Text: "ok", Status: mldomain.MessageAvailable,
		CreatedAt: testNow.Add(-time.Hour), ReadAt: &readAt, PackKey: packKey}
	f.messages.messages[msg.ID] = msg
	f.messages.threads[packKey] = activeThread(directPath(), msg)
	job := messageJob(msg.ID)
	job.Read = true
	if err := f.dispatcher.Execute(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(f.watermarks.calls) != 1 || !strings.Contains(f.watermarks.calls[0], "|mercadolivre|read|"+readAt.Format(time.RFC3339)) {
		t.Fatalf("watermarks = %v", f.watermarks.calls)
	}
}

func TestANotificationForAnUnknownSellerCreatesNothing(t *testing.T) {
	f := newHandlerFixture()
	job := messageJob("0123456789abcdef0008")
	job.SellerID = "999"
	if err := f.dispatcher.Execute(context.Background(), job); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("err = %v", err)
	}
	if len(f.conversations.rows) != 0 || len(f.contacts.rows) != 0 || f.metrics.count("consume/unknown_account") != 1 {
		t.Fatalf("rows created or not counted: %v", f.metrics.events)
	}
}

func TestTheDispatcherRefusesAJobKindWithoutAHandler(t *testing.T) {
	f := newHandlerFixture()
	for _, kind := range []mldomain.JobKind{mldomain.JobFetchQuestion, mldomain.JobFetchItem, "fetch_claim"} {
		err := f.dispatcher.Execute(context.Background(), &mldomain.Job{Kind: kind, SellerID: sellerID, ResourceID: "1"})
		if !errors.Is(err, ErrJobKindNotHandled) {
			t.Fatalf("%s: err = %v", kind, err)
		}
	}
	if got := f.metrics.count("consume/kind_not_handled"); got != 3 {
		t.Fatalf("kind_not_handled = %d", got)
	}
}

func TestAPackRefreshJobRefreshesOnlyTheSummary(t *testing.T) {
	f := newHandlerFixture()
	f.conversations.FindOrCreateByPack(context.Background(), mldomain.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", AccountID: "acct-1", ContactID: "ct-1", PackKey: packKey, BuyerMLUserID: buyerID,
	})
	job, err := mldomain.PackJob(sellerID, packKey, "stale:1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.dispatcher.Execute(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
	if len(f.conversations.summaries) != 1 || f.conversations.summaries[0].OrderStatus != "paid" {
		t.Fatalf("summaries = %+v", f.conversations.summaries)
	}
	if len(f.messages.reads) != 0 {
		t.Fatalf("a pack refresh reads no thread, read %v", f.messages.reads)
	}

	unknown, _ := mldomain.PackJob(sellerID, "2000000000000999", "stale:1")
	if err := f.dispatcher.Execute(context.Background(), &unknown); err != nil || len(f.conversations.summaries) != 1 {
		t.Fatalf("a pack without a conversation is skipped: %v", err)
	}
}

func TestReconciliationThreadsUseTheBackgroundShareAndPageTheThread(t *testing.T) {
	f := newHandlerFixture()
	var msgs []*mldomain.RemoteMessage
	for i := 0; i < 60; i++ {
		msgs = append(msgs, buyerMessage("0123456789abcdef1"+strings.Repeat("0", 2)+string(rune('a'+i%26))+string(rune('a'+i/26)), "m", testNow.Add(time.Duration(i)*time.Second)))
	}
	f.messages.threads[packKey] = activeThread(directPath(), msgs...)
	job, _ := mldomain.ThreadJob(sellerID, packKey, "reconcile:1")
	if err := f.dispatcher.Execute(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 60 || len(f.automation.inputs) != 1 {
		t.Fatalf("records=%d automation=%d", len(f.history.records), len(f.automation.inputs))
	}
	if f.automation.inputs[0].ProviderMessageID != msgs[59].ID {
		t.Fatal("the automation answers the newest message once")
	}
	for _, ctx := range f.messages.ctxs {
		if !mldomain.IsBackground(ctx) {
			t.Fatal("reconciliation reads must use the background share")
		}
	}
}

func TestAnOrderNotificationRefreshesAKnownPackOnly(t *testing.T) {
	f := newHandlerFixture()
	job := &mldomain.Job{Kind: mldomain.JobFetchOrder, SellerID: sellerID, ResourceID: "2000000000000001", NotificationID: "n-1"}
	if err := f.dispatcher.Execute(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(f.conversations.rows) != 0 {
		t.Fatal("an order without a conversation creates nothing")
	}
	f.conversations.FindOrCreateByPack(context.Background(), mldomain.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", AccountID: "acct-1", ContactID: "ct-1", PackKey: packKey, BuyerMLUserID: buyerID,
	})
	f.messages.threads[packKey] = &mldomain.Thread{Status: mldomain.ThreadStatusBlocked, Substatus: "blocked_by_cancelled_order", Path: directPath()}
	if err := f.dispatcher.Execute(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.conversations.rows {
		if c.PackSummary == nil || c.WindowOpen || c.MLSubstatus != "blocked_by_cancelled_order" {
			t.Fatalf("conversation = %+v", c)
		}
	}
	ship := &mldomain.Job{Kind: mldomain.JobFetchShipment, SellerID: sellerID, ResourceID: "40000000001", NotificationID: "n-2"}
	if err := f.dispatcher.Execute(context.Background(), ship); err != nil {
		t.Fatal(err)
	}
	if len(f.conversations.summaries) != 2 {
		t.Fatalf("summaries = %d", len(f.conversations.summaries))
	}
}

func TestAnAuthFailureThatCannotRecoverDropsAfterMarkingTheAccount(t *testing.T) {
	f := newHandlerFixture()
	f.messages.getErr = &mlErr{msg: "invalid_token", auth: true}
	f.oauth.refresh = func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		return nil, &mlErr{msg: "invalid_grant", auth: true}
	}
	err := f.dispatcher.Execute(context.Background(), messageJob("0123456789abcdef0009"))
	if !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if f.accounts.get("acct-1").Status != mldomain.StatusNeedsReconnect {
		t.Fatal("the account must be marked before the event is dropped")
	}
}

func TestAnAuthFailureThatRecoversIsRetried(t *testing.T) {
	f := newHandlerFixture()
	f.messages.getErr = &mlErr{msg: "invalid_token", auth: true}
	err := f.dispatcher.Execute(context.Background(), messageJob("0123456789abcdef0010"))
	if !errors.Is(err, errRetryWithFreshToken) {
		t.Fatalf("err = %v", err)
	}
	if f.accounts.get("acct-1").RefreshToken != "TG-2" {
		t.Fatal("the token must be refreshed before the retry")
	}
}
