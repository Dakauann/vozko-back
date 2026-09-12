package audience_usecase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/messaging"
)

// Alerts leave the analysis loop through a queue.
//
// The flush job walks every due container of every workspace SEQUENTIALLY, in
// one goroutine, every 30 seconds. Dispatching inline put an outbound WhatsApp
// call, and optionally a model call for the briefing, on that path: one
// workspace's alert delayed every other workspace's analysis in the same tick,
// and a slow provider stalled the lot.
//
// What does NOT move is the claim. That is a single conditional write and it is
// what decides who sends, so it stays inline; only the slow half is handed off.

type fakePublisher struct {
	published [][]byte
	err       error
}

func (f *fakePublisher) Publish(topic string, message []byte) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, message)
	return nil
}

func (f *fakePublisher) PublishWithDelay(topic string, message []byte, _ time.Duration) error {
	return f.Publish(topic, message)
}
func (f *fakePublisher) ValidateConnection() error { return f.err }

var _ messaging.MessageQueuePub = (*fakePublisher)(nil)

func TestAlertIsQueuedRatherThanSentInsideTheAnalysisPass(t *testing.T) {
	pub := &fakePublisher{}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(0)}}
	dispatcher := &fakeDispatcher{}
	uc := NewAlertEvaluator(AlertDeps{
		Rules: rules, Repo: newFakeRepo(), State: newFakeState(), Publisher: pub,
		Sender: NewAlertConsumer(AlertConsumerDeps{Dispatcher: dispatcher, Rules: rules, Clock: fixedClock{now}}),
		Clock:  fixedClock{now},
	})

	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1",
		[]*ca.Analysis{conversationAt("conv-bad", 41, 30, now)})

	if len(dispatcher.all()) != 0 {
		t.Error("the alert was sent inside the analysis pass instead of being queued")
	}
	if len(pub.published) != 1 {
		t.Fatalf("published %d alerts, want 1", len(pub.published))
	}

	// The claim still happened inline: that is what stops two replicas both
	// queueing the same firing, and moving it off this path would have made the
	// queue the thing deciding who sends.
	if rules.claimed != 1 {
		t.Errorf("claims = %d, want the firing claimed once before queueing", rules.claimed)
	}

	var queued ca.Alert
	if err := json.Unmarshal(pub.published[0], &queued); err != nil {
		t.Fatalf("the queued message is not a readable alert: %v", err)
	}
	if queued.Rule.ID != "rule-q" || queued.Observation.Value != 41 {
		t.Errorf("queued alert = %+v", queued)
	}
}

// A queue that is down must not silence an alert. Falling back to the inline
// send keeps the old behaviour, which is slower but not silent.
func TestAlertFallsBackToSendingWhenTheQueueRefuses(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(0)}}
	dispatcher := &fakeDispatcher{}
	uc := NewAlertEvaluator(AlertDeps{
		Rules: rules, Repo: newFakeRepo(), State: newFakeState(),
		Publisher: &fakePublisher{err: context.DeadlineExceeded},
		Sender:    NewAlertConsumer(AlertConsumerDeps{Dispatcher: dispatcher, Rules: rules, Clock: fixedClock{now}}),
		Clock:     fixedClock{now},
	})

	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1",
		[]*ca.Analysis{conversationAt("conv-bad", 41, 30, now)})

	if len(dispatcher.all()) != 1 {
		t.Fatalf("a failed publish sent %d alerts, want the inline fallback to send 1", len(dispatcher.all()))
	}
}

// With no publisher wired at all, the evaluator behaves exactly as it did
// before the queue existed. A deployment without a broker still alerts.
func TestAlertSendsInlineWithoutAPublisher(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(0)}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, newFakeState())

	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1",
		[]*ca.Analysis{conversationAt("conv-bad", 41, 30, now)})

	if len(dispatcher.all()) != 1 {
		t.Fatalf("sent %d alerts without a publisher, want 1", len(dispatcher.all()))
	}
}

// ---- the consumer ----

type fakeAck struct {
	acked    bool
	nacked   bool
	requeued bool
	count    int
}

func (a *fakeAck) Ack() error              { a.acked = true; return nil }
func (a *fakeAck) Nack(requeue bool) error { a.nacked, a.requeued = true, requeue; return nil }
func (a *fakeAck) DeliveryCount() int      { return a.count }

func TestAlertConsumerSendsWhatWasQueued(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	consumer := NewAlertConsumer(AlertConsumerDeps{Dispatcher: dispatcher})

	alert := ca.NewAlert(*qualityRule(0), ca.AlertObservation{
		Metric: ca.AlertMetricAttendanceQuality, Value: 41,
	}, now)
	raw, err := json.Marshal(alert)
	if err != nil {
		t.Fatal(err)
	}

	ack := &fakeAck{}
	consumer.Handle(raw, ack)

	sent := dispatcher.all()
	if len(sent) != 1 {
		t.Fatalf("the consumer sent %d alerts, want 1", len(sent))
	}
	if sent[0].Recipient != "5511999999999" {
		t.Errorf("delivery = %+v", sent[0])
	}
	if !ack.acked {
		t.Error("a delivered alert was not acknowledged")
	}
}

// A dispatch failure is acknowledged, NOT requeued.
//
// The inline path never retried, deliberately: a refused send cannot be told
// apart from one that arrived just before the connection dropped, and a
// duplicate alert at 3am is unrecoverable. Moving to a queue must not quietly
// turn that into at-least-once redelivery.
func TestAlertConsumerDoesNotRequeueAFailedSend(t *testing.T) {
	dispatcher := &fakeDispatcher{err: context.DeadlineExceeded}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(0)}}
	consumer := NewAlertConsumer(AlertConsumerDeps{Dispatcher: dispatcher, Rules: rules})

	raw, _ := json.Marshal(ca.NewAlert(*qualityRule(0), ca.AlertObservation{
		Metric: ca.AlertMetricAttendanceQuality, Value: 41,
	}, now))

	ack := &fakeAck{}
	consumer.Handle(raw, ack)

	if ack.requeued {
		t.Error("a failed alert was requeued, which is how one incident becomes many messages")
	}
	if !ack.acked && !ack.nacked {
		t.Error("the message was neither acked nor nacked, so it will be redelivered forever")
	}
	// And the reason is stored where the operator looks for it.
	if len(rules.failures) == 0 {
		t.Error("the failure was not recorded on the rule")
	}
}

func TestAlertConsumerDropsAnUnreadableMessage(t *testing.T) {
	consumer := NewAlertConsumer(AlertConsumerDeps{Dispatcher: &fakeDispatcher{}})
	ack := &fakeAck{}
	consumer.Handle([]byte("not json"), ack)
	if ack.requeued {
		t.Error("an unreadable message was requeued and will never parse")
	}
}

var _ messaging.MessageAck = (*fakeAck)(nil)
