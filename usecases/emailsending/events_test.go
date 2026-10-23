package emailsending

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	webhook_usecase "vozko/usecases/webhook"
)

type recordingUnsubscriber struct {
	calls []string
	err   error
}

func (u *recordingUnsubscriber) UnsubscribeByComplaint(_ context.Context, ws string, a emailaddress.Address, id string) error {
	u.calls = append(u.calls, ws+"|"+string(a)+"|"+id)
	return u.err
}

type recordingObserver struct {
	events []es.EventKind
	err    error
}

func (o *recordingObserver) OnMessageEvent(_ context.Context, _ es.Message, ev es.DeliveryEvent, _ es.EventEffect) error {
	o.events = append(o.events, ev.Kind)
	return o.err
}

type recordingInbound struct{ notices []es.InboundNotice }

func (r *recordingInbound) HandleInbound(_ context.Context, n es.InboundNotice) error {
	r.notices = append(r.notices, n)
	return nil
}

func sentMessage(t *testing.T, h *harness) *es.Message {
	t.Helper()
	msg, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestEventsUnknownMessageIsDropped(t *testing.T) {
	h := newHarness(t)
	err := h.m.Events.Apply(context.Background(), es.Notification{Delivery: &es.DeliveryEvent{Kind: es.EventDelivery, ProviderMessageID: "nope", OccurredAt: testNow}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventsComplaintSuppressesUnsubscribesAndNotifies(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	msg := sentMessage(t, h)
	unsub := &recordingUnsubscriber{}
	observer := &recordingObserver{err: errors.New("projection lagging")}
	h.m.Events.SetUnsubscriber(unsub)
	h.m.Events.AddObserver(observer)
	ev := es.DeliveryEvent{Kind: es.EventComplaint, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow.Add(time.Hour), Complaint: &es.Complaint{FeedbackType: "abuse"}}

	if err := h.m.Events.Apply(ctx, es.Notification{Delivery: &ev}); err != nil {
		t.Fatal(err)
	}
	found, _ := h.suppressions.Suppressed(ctx, wsID, []emailaddress.Address{msg.Address})
	if found[msg.Address] != es.SuppressComplaint {
		t.Fatalf("suppressions = %v", found)
	}
	if len(unsub.calls) != 1 || unsub.calls[0] != wsID+"|cliente@example.com|"+msg.ID {
		t.Fatalf("unsubscribe calls = %v", unsub.calls)
	}
	stored, _ := h.messages.Get(ctx, msg.ID)
	if stored.ComplainedAt == nil || stored.UnsubscribedAt == nil {
		t.Fatalf("message = %+v", stored)
	}
	if err := h.m.Events.Apply(ctx, es.Notification{Delivery: &ev}); err != nil {
		t.Fatal(err)
	}
	if len(observer.events) != 1 {
		t.Fatalf("observers only see new events: %v", observer.events)
	}
	if len(unsub.calls) != 2 || h.suppressions.adds != 2 {
		t.Fatal("compliance side effects are replayed idempotently on duplicates")
	}
}

func TestEventsComplaintWithoutUnsubscriberFailsForRetry(t *testing.T) {
	h, _ := readyHarness(t)
	msg := sentMessage(t, h)
	ev := es.DeliveryEvent{Kind: es.EventComplaint, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow, Complaint: &es.Complaint{FeedbackType: "abuse"}}
	err := h.m.Events.Apply(context.Background(), es.Notification{Delivery: &ev})
	if !errors.Is(err, ErrNoUnsubscriber) || ClassifyFailure(err) != webhook_usecase.DispositionRetry {
		t.Fatalf("err = %v", err)
	}
}

func TestEventsHealMessagesThroughTheTag(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	queued := &es.Message{WorkspaceID: wsID, Address: "cliente@example.com", Status: es.MessageQueued, RunKind: es.RunTriggered}
	_ = h.messages.Record(ctx, queued)
	ev := es.DeliveryEvent{Kind: es.EventDelivery, ProviderMessageID: "ses-late", OccurredAt: testNow, Tags: map[string]string{es.MessageIDTag: queued.ID}}
	if err := h.m.Events.Apply(ctx, es.Notification{Delivery: &ev}); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.messages.Get(ctx, queued.ID)
	if stored.ProviderMessageID != "ses-late" || stored.Status != es.MessageDelivered {
		t.Fatalf("message = %+v", stored)
	}
	spoof := es.DeliveryEvent{Kind: es.EventDelivery, ProviderMessageID: "ses-other", OccurredAt: testNow, Tags: map[string]string{es.MessageIDTag: queued.ID}}
	if err := h.m.Events.Apply(ctx, es.Notification{Delivery: &spoof}); err != nil {
		t.Fatal(err)
	}
	if stored, _ = h.messages.Get(ctx, queued.ID); stored.ProviderMessageID != "ses-late" {
		t.Fatal("a tag never rebinds a message that already has a provider id")
	}
}

func TestEventsHardBounceSuppresses(t *testing.T) {
	h, _ := readyHarness(t)
	msg := sentMessage(t, h)
	ev := es.DeliveryEvent{Kind: es.EventBounce, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow, Bounce: &es.Bounce{Kind: es.BounceHard}}
	if err := h.m.Events.Apply(context.Background(), es.Notification{Delivery: &ev}); err != nil {
		t.Fatal(err)
	}
	found, _ := h.suppressions.Suppressed(context.Background(), wsID, []emailaddress.Address{msg.Address})
	if found[msg.Address] != es.SuppressHardBounce {
		t.Fatalf("suppressions = %v", found)
	}
}

func TestEventsTenantStatus(t *testing.T) {
	cases := []struct {
		name     string
		initial  es.AccountStatus
		event    es.TenantStatusEvent
		want     es.AccountStatus
		episode  bool
		resolved bool
	}{
		{"provider pause", es.AccountActive, es.TenantStatusEvent{Origin: es.OriginProvider, Status: es.TenantDisabled, Cause: "bounces"}, es.AccountPausedByProvider, true, false},
		{"provider reinstates", es.AccountPausedByProvider, es.TenantStatusEvent{Origin: es.OriginProvider, Status: es.TenantReinstated}, es.AccountActive, false, true},
		{"provider enable keeps a platform pause", es.AccountPausedByPlatform, es.TenantStatusEvent{Origin: es.OriginProvider, Status: es.TenantEnabled}, es.AccountPausedByPlatform, false, true},
		{"console pause", es.AccountActive, es.TenantStatusEvent{Origin: es.OriginPlatform, Status: es.TenantDisabled, Cause: "manual"}, es.AccountPausedByPlatform, false, false},
		{"platform echo of a provider pause", es.AccountPausedByProvider, es.TenantStatusEvent{Origin: es.OriginPlatform, Status: es.TenantEnabled}, es.AccountPausedByProvider, false, false},
		{"console unpause", es.AccountPausedByPlatform, es.TenantStatusEvent{Origin: es.OriginPlatform, Status: es.TenantEnabled}, es.AccountActive, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := provisioned(t)
			ctx := context.Background()
			acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
			acct.Status = tc.initial
			_ = h.accounts.Update(ctx, acct)
			ev := tc.event
			ev.TenantName = acct.TenantName
			if err := h.m.Events.Apply(ctx, es.Notification{TenantStatus: &ev}); err != nil {
				t.Fatal(err)
			}
			got, _ := h.accounts.GetByWorkspace(ctx, wsID)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q", got.Status, tc.want)
			}
			published := false
			for _, k := range h.alerts.keys() {
				if k == tenantPausedKey(wsID) {
					published = true
				}
			}
			resolved := false
			for _, k := range h.alerts.resolved {
				if k == tenantPausedKey(wsID) {
					resolved = true
				}
			}
			if published != tc.episode || resolved != tc.resolved {
				t.Fatalf("published = %v resolved = %v", published, resolved)
			}
		})
	}
	h := newHarness(t)
	if err := h.m.Events.Apply(context.Background(), es.Notification{TenantStatus: &es.TenantStatusEvent{TenantName: "ghost", Origin: es.OriginProvider, Status: es.TenantDisabled}}); err != nil {
		t.Fatalf("unknown tenants are dropped: %v", err)
	}
}

func TestEventsFindings(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
	low := es.FindingEvent{TenantName: acct.TenantName, Impact: es.FindingLow, Type: "COMPLAINT", Open: true}
	high := es.FindingEvent{TenantName: acct.TenantName, Impact: es.FindingHigh, Type: "BOUNCE", Open: true, Description: "rate"}
	resolved := high
	resolved.Open = false
	for _, f := range []es.FindingEvent{low, high, resolved} {
		f := f
		if err := h.m.Events.Apply(ctx, es.Notification{Finding: &f}); err != nil {
			t.Fatal(err)
		}
	}
	keys := h.alerts.keys()
	if len(keys) != 1 || keys[0] != "email_reputation_finding:"+wsID+":bounce" {
		t.Fatalf("drafts = %v", keys)
	}
	if h.alerts.resolved[len(h.alerts.resolved)-1] != "email_reputation_finding:"+wsID+":bounce" {
		t.Fatalf("resolved = %v", h.alerts.resolved)
	}
}

func TestEventsInbound(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	notice := es.InboundNotice{ObjectKey: "inbound/abc", MessageID: "abc"}
	if err := h.m.Events.Apply(ctx, es.Notification{Inbound: &notice}); !errors.Is(err, ErrNoInboundHandler) {
		t.Fatalf("err = %v", err)
	}
	handler := &recordingInbound{}
	h.m.Events.SetInboundHandler(handler)
	if err := h.m.Events.Apply(ctx, es.Notification{Inbound: &notice}); err != nil || len(handler.notices) != 1 {
		t.Fatalf("err = %v notices = %v", err, handler.notices)
	}
	if err := h.m.Events.Apply(ctx, es.Notification{}); !errors.Is(err, ErrEmptyNotification) {
		t.Fatalf("err = %v", err)
	}
}

func TestQueuedNotificationHandling(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	q := QueuedNotification{MessageID: "sns-1", Message: "{}"}
	if q.DedupKey() != "email-sns:sns-1" || (QueuedNotification{}).DedupKey() != "" {
		t.Fatal("dedup key")
	}
	err := HandleQueued(ctx, fakeDecoder{err: errors.New("unknown shape")}, h.m.Events, q)
	if !errors.Is(err, ErrUndecodableEvent) || ClassifyFailure(err) != webhook_usecase.DispositionDrop {
		t.Fatalf("err = %v", err)
	}
	notice := es.InboundNotice{ObjectKey: "inbound/x"}
	err = HandleQueued(ctx, fakeDecoder{n: es.Notification{Inbound: &notice}}, h.m.Events, q)
	if !errors.Is(err, ErrNoInboundHandler) || ClassifyFailure(err) != webhook_usecase.DispositionRetry {
		t.Fatalf("err = %v", err)
	}
	if ClassifyFailure(ErrEmptyNotification) != webhook_usecase.DispositionDrop {
		t.Fatal("empty notifications are dropped")
	}
}

type recordingPublisher struct {
	topic   string
	payload []byte
}

func (p *recordingPublisher) Publish(topic string, payload []byte) error {
	p.topic, p.payload = topic, payload
	return nil
}

func TestNotificationIntake(t *testing.T) {
	pub := &recordingPublisher{}
	intake, err := NewNotificationIntake(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := intake.Accept("", "arn", "{}"); err == nil {
		t.Fatal("missing id must fail")
	}
	if err := intake.Accept("sns-1", "arn:topic", `{"eventType":"Send"}`); err != nil {
		t.Fatal(err)
	}
	if pub.topic != "webhook.email.notification" || len(pub.payload) == 0 {
		t.Fatalf("published = %q %s", pub.topic, pub.payload)
	}
	if _, err := NewNotificationIntake(nil); err == nil {
		t.Fatal("nil publisher must fail")
	}
}

func TestAProviderNoticeIsAppliedAsNothingToDo(t *testing.T) {
	h := newHarness(t)
	notice := es.Notification{Notice: &es.ProviderNotice{Kind: es.NoticeTopicValidated}}
	if err := h.m.Events.Apply(context.Background(), notice); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(h.messages.rows) != 0 || len(h.alerts.keys()) != 0 || len(h.ops.alerts) != 0 {
		t.Fatal("a provider notice changes nothing")
	}
}

func TestEventsFeedTheAddressReputationOnEveryDeliveryOutcome(t *testing.T) {
	h, _ := readyHarness(t)
	msg := sentMessage(t, h)
	ctx := context.Background()
	for _, ev := range []es.DeliveryEvent{
		{Kind: es.EventDelivery, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow},
		{Kind: es.EventDelivery, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow},
		{Kind: es.EventClick, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow.Add(time.Minute), Link: "https://loja.test"},
	} {
		ev := ev
		if err := h.m.Events.Apply(ctx, es.Notification{Delivery: &ev}); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.reputation.kinds(); len(got) != 3 || got[0] != es.EventDelivery || got[2] != es.EventClick {
		t.Fatalf("reputation saw %v, a replayed notification is passed again so its idempotent write heals a lost one", got)
	}
}

func TestEventsSuppressASoftBounceStreakForTheWorkspace(t *testing.T) {
	h, _ := readyHarness(t)
	msg := sentMessage(t, h)
	h.reputation.answer = es.SuppressSoftBounce
	ev := es.DeliveryEvent{Kind: es.EventBounce, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow, Bounce: &es.Bounce{Kind: es.BounceSoft, SubType: "MailboxFull"}}
	if err := h.m.Events.Apply(context.Background(), es.Notification{Delivery: &ev}); err != nil {
		t.Fatal(err)
	}
	found, _ := h.suppressions.Suppressed(context.Background(), wsID, []emailaddress.Address{msg.Address})
	if found[msg.Address] != es.SuppressSoftBounce {
		t.Fatalf("suppressions = %v", found)
	}
}

func TestEventsRetryWhenTheReputationCannotBeWritten(t *testing.T) {
	h, _ := readyHarness(t)
	msg := sentMessage(t, h)
	h.reputation.err = errors.New("db down")
	ev := es.DeliveryEvent{Kind: es.EventBounce, ProviderMessageID: msg.ProviderMessageID, OccurredAt: testNow, Bounce: &es.Bounce{Kind: es.BounceHard}}
	if err := h.m.Events.Apply(context.Background(), es.Notification{Delivery: &ev}); err == nil {
		t.Fatal("a hard bounce that did not reach the shared list must be retried, never settled")
	}
}
