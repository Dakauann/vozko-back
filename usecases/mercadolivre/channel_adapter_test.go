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
	"vozko/domain/webhook"
)

type fakeFetcher struct {
	files map[string]*mldomain.AttachmentDownload
	urls  []string
}

func (f *fakeFetcher) Fetch(_ context.Context, url string, max int64) (*mldomain.AttachmentDownload, error) {
	f.urls = append(f.urls, url)
	if d, ok := f.files[url]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

type adapterFixture struct {
	*handlerFixture
	adapter      *ChannelAdapter
	fetcher      *fakeFetcher
	refreshQueue *fakePublisher
	conv         *mldomain.Conversation
}

func newAdapterFixture(t *testing.T, mediated bool, open bool) *adapterFixture {
	t.Helper()
	h := newHandlerFixture()
	contact, _ := h.contacts.FindOrCreateByUserID(context.Background(), mldomain.FindOrCreateContactInput{WorkspaceID: "ws-1", AccountID: "acct-1", MLUserID: buyerID, Nickname: "COMPRADOR"})
	conv, _, _ := h.conversations.FindOrCreateByPack(context.Background(), mldomain.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", AccountID: "acct-1", ContactID: contact.ID, PackKey: packKey, BuyerMLUserID: buyerID,
	})
	budget := 0
	if mediated {
		budget = 1
	}
	_ = h.conversations.ApplyStatus(context.Background(), conv.ID, mldomain.ThreadState{
		Status: mldomain.ThreadStatusActive, AgentMediated: boolPtr(mediated), OutboundBudget: budget, WindowOpen: open, ReadAt: testNow.Add(-time.Hour),
	})
	inbound := testNow.Add(-30 * time.Minute)
	_ = h.conversations.RecordInbound(context.Background(), conv.ID, inbound)
	h.messages.sendOut = &mldomain.SentMessage{ID: "94353d192b9640e8b1ed3c77aa406f39", Status: mldomain.MessageAvailable}
	fetcher := &fakeFetcher{files: map[string]*mldomain.AttachmentDownload{}}
	tokens := NewTokenSource(h.accounts, h.oauth, fakeClassifier{}, h.alerts, h.metrics)
	tokens.now = fixedClock
	svc := Services{Messages: h.messages, Attachments: h.attachments, Errors: fakeClassifier{}}
	threads := h.dispatcher.handlers[mldomain.JobFetchThread].(*ThreadSync)
	refreshQueue := newFakePublisher()
	adapter := NewChannelAdapter(ChannelAdapterDeps{
		Accounts: h.accounts, Contacts: h.contacts, Conversations: h.conversations, Services: svc,
		Tokens: tokens, Threads: threads, Refresh: NewJobPublisher(refreshQueue), Media: fetcher, Metrics: h.metrics,
	})
	adapter.now = fixedClock
	return &adapterFixture{handlerFixture: h, adapter: adapter, fetcher: fetcher, refreshQueue: refreshQueue, conv: h.conversations.get(conv.ID)}
}

func (f *adapterFixture) entry(t *testing.T) *conversation.EntryContext {
	t.Helper()
	ec, err := f.adapter.ResolveEntry(context.Background(), f.conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ec
}

func TestTheAdapterDescribesTheChannel(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	if f.adapter.EntryType() != shared.EntryTypeMercadoLivre || f.adapter.Capabilities().MaxTextRunes != 350 {
		t.Fatal("descriptor not carried")
	}
	var _ conversation.CapabilitiesAdapter = f.adapter
	ec := f.entry(t)
	if ec.ContactRef != buyerID || ec.ContactHandle != "COMPRADOR" || ec.AccountID != "acct-1" || ec.WorkspaceID != "ws-1" || ec.LastInboundAt == nil {
		t.Fatalf("entry = %+v", ec)
	}
}

func TestWindowStateFollowsTheMirroredThread(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *adapterFixture)
		open   bool
		reason conversation.WindowClosedReason
		detail string
	}{
		{"open thread", nil, true, "", ""},
		{"blocked by mediation", func(f *adapterFixture) {
			_ = f.conversations.ApplyStatus(context.Background(), f.conv.ID, mldomain.ThreadState{
				Status: "blocked", Substatus: "blocked_by_mediation", AgentMediated: boolPtr(false), ReadAt: testNow,
			})
		}, false, conversation.WindowReasonProviderBlocked, "blocked_by_mediation"},
		{"mediation unknown", func(f *adapterFixture) {
			_ = f.conversations.ApplyStatus(context.Background(), f.conv.ID, mldomain.ThreadState{Status: "active", ReadAt: testNow})
		}, false, conversation.WindowReasonChannelUnavailable, ""},
		{"account needs reconnect", func(f *adapterFixture) {
			_ = f.accounts.UpdateStatus(context.Background(), "acct-1", mldomain.StatusNeedsReconnect, "x")
		}, false, conversation.WindowReasonChannelUnavailable, ""},
		{"send permission lost", func(f *adapterFixture) {
			_ = f.accounts.UpdateCapabilities(context.Background(), "acct-1", mldomain.AccountCapabilities{CanReadMessages: true})
		}, false, conversation.WindowReasonChannelUnavailable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdapterFixture(t, false, true)
			if tc.mutate != nil {
				tc.mutate(f)
			}
			w, err := f.adapter.WindowState(context.Background(), f.entry(t))
			if err != nil {
				t.Fatal(err)
			}
			if w.Open != tc.open || w.Reason != tc.reason || w.ProviderDetail != tc.detail {
				t.Fatalf("window = %+v", w)
			}
		})
	}
}

func TestAMediatedThreadClosesOnceTheBudgetIsSpent(t *testing.T) {
	f := newAdapterFixture(t, true, true)
	ec := f.entry(t)
	if w, _ := f.adapter.WindowState(context.Background(), ec); !w.Open {
		t.Fatalf("window = %+v", w)
	}
	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Seu pedido sai hoje."}); err != nil {
		t.Fatal(err)
	}
	if len(f.messages.sent) != 1 || f.messages.sent[0].ToUserID != agentID {
		t.Fatalf("sent = %+v", f.messages.sent)
	}
	w, _ := f.adapter.WindowState(context.Background(), ec)
	if w.Open || w.Reason != conversation.WindowReasonBudgetExhausted {
		t.Fatalf("window = %+v", w)
	}
	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "De novo"}); !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("second send = %v", err)
	}
	if len(f.messages.sent) != 1 {
		t.Fatal("a spent budget must never reach Mercado Livre")
	}
}

func TestADirectThreadSendsToTheBuyerAndStoresModeration(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.messages.sendOut = &mldomain.SentMessage{ID: "94353d192b9640e8b1ed3c77aa406f39", Status: mldomain.MessagePendingReview, PendingReview: true}
	out, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "Olá! Tudo certo."})
	if err != nil {
		t.Fatal(err)
	}
	if f.messages.sent[0].ToUserID != buyerID || f.messages.sent[0].PackKey != packKey || out.ProviderMessageID != "94353d192b9640e8b1ed3c77aa406f39" {
		t.Fatalf("sent=%+v out=%+v", f.messages.sent, out)
	}
	moderation, ok := out.Metadata[ModerationMetadataKey].(map[string]string)
	if !ok || moderation["status"] != "pending_review" || out.DeliveryStatus != conversation.DeliveryStatusNone {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestASuccessfulSendRecordsTheOutboundClock(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	if _, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "Olá! Tudo certo."}); err != nil {
		t.Fatal(err)
	}
	if f.conversations.outbound != 1 {
		t.Fatalf("outbound clock recorded %d times, want 1", f.conversations.outbound)
	}
}

func TestARefusedSendLeavesTheOutboundClockAlone(t *testing.T) {
	f := newAdapterFixture(t, false, false)
	if _, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "Olá! Tudo certo."}); err == nil {
		t.Fatal("a closed thread must refuse the send")
	}
	if f.conversations.outbound != 0 {
		t.Fatalf("outbound clock recorded %d times, want none", f.conversations.outbound)
	}
}

func TestARejectedSendIsStoredAsFailed(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.messages.sendOut = &mldomain.SentMessage{ID: "94353d192b9640e8b1ed3c77aa406f39", Status: mldomain.MessageModerated, ModerationStatus: "rejected", ModerationReason: "PERSONAL_DATA"}
	out, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "meu zap"})
	if err != nil || out.DeliveryStatus != conversation.DeliveryStatusFailed {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestTextRulesRefuseBeforeAnyClaimOrCall(t *testing.T) {
	f := newAdapterFixture(t, true, true)
	for _, body := range []string{"", strings.Repeat("a", 351), "Total 10\U000020AC"} {
		if _, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: body}); err == nil {
			t.Fatalf("%q accepted", body)
		}
	}
	if f.conversations.claims != 0 || len(f.messages.sent) != 0 {
		t.Fatal("an invalid text must not claim the budget nor call Mercado Livre")
	}
}

func TestADefiniteRefusalReleasesTheClaimAndABlockClosesTheThread(t *testing.T) {
	f := newAdapterFixture(t, true, true)
	f.messages.sendErr = &mlErr{msg: "blocked", blocked: true, substatus: "blocked_by_time"}
	_, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v", err)
	}
	conv := f.conversations.get(f.conv.ID)
	if f.conversations.releases != 1 || conv.OutboundSinceInbound != 0 || conv.WindowOpen || conv.MLSubstatus != "blocked_by_time" {
		t.Fatalf("conv = %+v releases = %d", conv, f.conversations.releases)
	}
	w, _ := f.adapter.WindowState(context.Background(), f.entry(t))
	if w.Reason != conversation.WindowReasonProviderBlocked || w.ProviderDetail != "blocked_by_time" {
		t.Fatalf("window = %+v", w)
	}
}

func TestAnUnknownBlockRereadsTheThread(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.messages.sendErr = &mlErr{msg: "blocked", blocked: true}
	f.messages.threads[packKey] = &mldomain.Thread{Status: "blocked", Substatus: "blocked_by_buyer", Path: directPath()}
	if _, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "oi"}); !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v", err)
	}
	if conv := f.conversations.get(f.conv.ID); conv.MLSubstatus != "blocked_by_buyer" || conv.WindowOpen {
		t.Fatalf("conv = %+v", conv)
	}
}

func TestAnAmbiguousFailureKeepsTheClaim(t *testing.T) {
	f := newAdapterFixture(t, true, true)
	f.messages.sendErr = &mlErr{msg: "timeout", ambiguous: true}
	if _, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("a timeout is not a success")
	}
	if f.conversations.releases != 0 || f.conversations.get(f.conv.ID).OutboundSinceInbound != 1 {
		t.Fatal("an ambiguous failure must keep the claim; the buyer may have received it")
	}
}

func TestAClaimThatCannotBeWrittenRefusesTheSend(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.conversations.claimErr = errors.New("db down")
	if _, err := f.adapter.SendText(context.Background(), f.entry(t), conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("sent without a claim")
	}
	if len(f.messages.sent) != 0 {
		t.Fatal("Mercado Livre was called without a claim")
	}
}

func TestSendMediaUploadsThenClaimsThenSends(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.fetcher.files["https://cdn.example/nota.pdf"] = &mldomain.AttachmentDownload{Data: []byte("%PDF"), ContentType: "application/pdf"}
	out, err := f.adapter.SendMedia(context.Background(), f.entry(t), conversation.SendMediaRequest{Kind: "document", URL: "https://cdn.example/nota.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.attachments.uploads) != 1 || f.attachments.uploads[0].FileName != "nota.pdf" || f.attachments.uploads[0].ContentType != "application/pdf" {
		t.Fatalf("uploads = %+v", f.attachments.uploads)
	}
	sent := f.messages.sent[0]
	if sent.Text != "" || len(sent.AttachmentIDs) != 1 || sent.AttachmentIDs[0] != "att-nota.pdf" || out.ProviderMessageID == "" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestSendMediaRefusesWhatMercadoLivreDoesNotAccept(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	cases := []conversation.SendMediaRequest{
		{Kind: "video", Bytes: []byte("x"), MIMEType: "video/mp4", FileName: "v.mp4"},
		{Kind: "image", Bytes: make([]byte, mldomain.MaxAttachmentBytes+1), MIMEType: "image/png", FileName: "big.png"},
		{Kind: "image", Bytes: []byte("x"), MIMEType: "image/png", FileName: "a.png", Caption: "Total 10\U000020AC"},
		{Kind: "image"},
	}
	for i, req := range cases {
		if _, err := f.adapter.SendMedia(context.Background(), f.entry(t), req); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	if len(f.attachments.uploads) != 0 || f.conversations.claims != 0 {
		t.Fatal("refused media must not upload nor claim")
	}
}

func TestAStaleThreadStatusQueuesOneReread(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	_ = f.conversations.ApplyStatus(context.Background(), f.conv.ID, mldomain.ThreadState{
		Status: "active", AgentMediated: boolPtr(false), WindowOpen: true, ReadAt: testNow.Add(-7 * time.Hour),
	})
	for i := 0; i < 3; i++ {
		w, err := f.adapter.WindowState(context.Background(), f.entry(t))
		if err != nil || !w.Open {
			t.Fatalf("the last known state stands until the re-read lands: %+v %v", w, err)
		}
	}
	if len(f.messages.reads) != 0 {
		t.Fatal("the composer must never wait on Mercado Livre")
	}
	jobs := f.refreshQueue.published[webhook.TopicMercadoLivreMessage]
	if len(jobs) != 3 {
		t.Fatalf("queued = %d", len(jobs))
	}
	keys := map[string]bool{}
	for _, raw := range jobs {
		var job mldomain.Job
		if err := json.Unmarshal(raw, &job); err != nil || job.Kind != mldomain.JobFetchThread || job.ResourceID != packKey {
			t.Fatalf("job = %+v %v", job, err)
		}
		keys[job.IdempotencyKey()] = true
	}
	if len(keys) != 1 {
		t.Fatalf("repeated reads of one stale thread must share one dedup key: %v", keys)
	}

	fresh := newAdapterFixture(t, false, true)
	if _, err := fresh.adapter.WindowState(context.Background(), fresh.entry(t)); err != nil {
		t.Fatal(err)
	}
	if fresh.refreshQueue.total() != 0 {
		t.Fatal("a fresh status queues nothing")
	}
}
