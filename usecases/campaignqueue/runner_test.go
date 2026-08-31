package campaignqueue

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/messaging"
)

// ---------------------------------------------------------------- doubles

type fakeSub struct {
	mu       sync.Mutex
	handlers map[string]func([]byte, messaging.MessageAck)
	deleted  []string
	stopped  []string
	lengths  map[string]int
}

func newFakeSub() *fakeSub {
	return &fakeSub{handlers: map[string]func([]byte, messaging.MessageAck){}, lengths: map[string]int{}}
}

func (f *fakeSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[topic] = handler
	return nil
}

func (f *fakeSub) DeleteQueue(topic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, topic)
	delete(f.handlers, topic)
	return nil
}

func (f *fakeSub) StopConsumer(topic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, topic)
	return nil
}

func (f *fakeSub) ValidateConnection() error { return nil }

func (f *fakeSub) GetQueueLength(topic string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lengths[topic], nil
}

func (f *fakeSub) deliver(t *testing.T, topic string, msg Message) *fakeAck {
	t.Helper()
	f.mu.Lock()
	handler, ok := f.handlers[topic]
	f.mu.Unlock()
	if !ok {
		t.Fatalf("nothing subscribed to %s", topic)
	}
	raw, _ := json.Marshal(msg)
	ack := &fakeAck{}
	handler(raw, ack)
	return ack
}

type fakePub struct {
	mu       sync.Mutex
	delayed  []time.Duration
	published int
	err      error
}

func (f *fakePub) Publish(string, []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.published++
	return nil
}

func (f *fakePub) PublishWithDelay(_ string, _ []byte, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delayed = append(f.delayed, d)
	return nil
}

func (f *fakePub) ValidateConnection() error { return nil }

type fakeAck struct {
	acked        bool
	nacked       bool
	nackRequeue  bool
}

func (a *fakeAck) Ack() error { a.acked = true; return nil }
func (a *fakeAck) Nack(requeue bool) error {
	a.nacked = true
	a.nackRequeue = requeue
	return nil
}
func (a *fakeAck) DeliveryCount() int { return 1 }

type fakeStatus struct {
	running   []string
	completed []string
	swapOK    bool
}

func (f *fakeStatus) ListRunningCampaignIDs() ([]string, error) { return f.running, nil }
func (f *fakeStatus) CompleteCampaign(id string) (bool, error) {
	f.completed = append(f.completed, id)
	return f.swapOK, nil
}

type fakePending struct{ n int64 }

func (f *fakePending) CountPendingEntries(string) (int64, error) { return f.n, nil }

type memShared struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemShared() *memShared { return &memShared{values: map[string]string{}} }

func (m *memShared) SetNX(k, v string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[k]; ok {
		return false, nil
	}
	m.values[k] = v
	return true, nil
}
func (m *memShared) SetString(k, v string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[k] = v
	return nil
}
func (m *memShared) GetString(k string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[k], nil
}
func (m *memShared) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}
func (m *memShared) Exists(k string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.values[k]
	return ok, nil
}
func (m *memShared) Incr(k string) (int64, error) { return m.IncrBy(k, 1) }
func (m *memShared) Decr(k string) (int64, error) { return m.IncrBy(k, -1) }
func (m *memShared) IncrBy(k string, n int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, _ := strconv.ParseInt(m.values[k], 10, 64)
	v += n
	m.values[k] = strconv.FormatInt(v, 10)
	return v, nil
}
func (m *memShared) DecrBy(k string, n int64) (int64, error)          { return m.IncrBy(k, -n) }
func (m *memShared) IncrWithTTL(k string, _ time.Duration) (int64, error) { return m.IncrBy(k, 1) }
func (m *memShared) TryIncr(string, int64) (bool, error)              { return true, nil }
func (m *memShared) TryIncrBy(string, int64, int64) (bool, error)     { return true, nil }
func (m *memShared) SAdd(string, ...string) error                     { return nil }
func (m *memShared) SRem(string, ...string) error                     { return nil }
func (m *memShared) SMembers(string) ([]string, error)                { return nil, nil }
func (m *memShared) Publish(string, []byte) error                     { return nil }
func (m *memShared) Subscribe(context.Context, string, func([]byte))  {}
func (m *memShared) HSet(string, string, string) error                { return nil }
func (m *memShared) HDel(string, string) error                        { return nil }
func (m *memShared) HGetAll(string) (map[string]string, error)        { return nil, nil }
func (m *memShared) HIncrBy(string, string, int64) (int64, error)     { return 0, nil }
func (m *memShared) Expire(string, time.Duration) (bool, error)       { return true, nil }

// ---------------------------------------------------------------- harness

var testNS = campaign.Namespace{Topic: "test_dispatch", Key: "campaign:test"}

type rig struct {
	runner  *Runner
	sub     *fakeSub
	pub     *fakePub
	shared  *memShared
	status  *fakeStatus
	pending *fakePending
	result  Result
	calls   int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		sub: newFakeSub(), pub: &fakePub{}, shared: newMemShared(),
		status: &fakeStatus{swapOK: true}, pending: &fakePending{},
		result: Done,
	}
	r.runner = New(r.sub, r.pub, r.shared, r.status, r.pending,
		Config{Namespace: testNS, PauseRequeueDelay: time.Second, Logf: func(string, ...any) {}},
		func(Message) Result {
			r.calls++
			return r.result
		})
	return r
}

func (r *rig) topic(id string) string { return testNS.DispatchTopic(id) }

// ---------------------------------------------------------------- outcomes

// The distinction that matters most: a Drop resolves an entry and COUNTS it, a
// RetryLater leaves it pending and must NOT — or the campaign completes while
// work is still queued.
func TestOutcomeCounting(t *testing.T) {
	cases := []struct {
		name        string
		result      Result
		wantAck     bool
		wantNack    bool
		wantCounted bool
		wantDelayed bool
	}{
		{"done counts", Done, true, false, true, false},
		{"drop counts", Drop, true, false, true, false},
		{"retry does not count", RetryLater(time.Second), true, false, false, true},
		{"requeue nacks and does not count", Requeue, false, true, false, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.result = c.result
			if err := r.runner.SubscribeToCampaign("camp"); err != nil {
				t.Fatal(err)
			}
			r.runner.SetCounter("camp", 2)

			ack := r.sub.deliver(t, r.topic("camp"), Message{CampaignID: "camp", EntryID: "e1"})

			if ack.acked != c.wantAck || ack.nacked != c.wantNack {
				t.Fatalf("ack=%v nack=%v, want ack=%v nack=%v",
					ack.acked, ack.nacked, c.wantAck, c.wantNack)
			}
			remaining, _ := r.shared.GetString(testNS.RemainingKey("camp"))
			wantRemaining := "2"
			if c.wantCounted {
				wantRemaining = "1"
			}
			if remaining != wantRemaining {
				t.Fatalf("remaining = %s, want %s", remaining, wantRemaining)
			}
			if got := len(r.pub.delayed) > 0; got != c.wantDelayed {
				t.Fatalf("delayed=%v, want %v", got, c.wantDelayed)
			}
		})
	}
}

// Reaching zero completes the campaign exactly once and tears the queue down.
func TestCampaignCompletesAtZero(t *testing.T) {
	r := newRig(t)
	_ = r.runner.SubscribeToCampaign("camp")
	r.runner.SetCounter("camp", 1)

	r.sub.deliver(t, r.topic("camp"), Message{CampaignID: "camp", EntryID: "e1"})

	if len(r.status.completed) != 1 {
		t.Fatalf("completed %d times, want 1", len(r.status.completed))
	}
	if len(r.sub.deleted) != 1 {
		t.Fatal("the queue was not deleted on completion")
	}
	if r.runner.IsSubscribed("camp") {
		t.Fatal("still subscribed after completion")
	}
}

// Two replicas finishing the last entry at once must not both declare
// completion. The compare-and-swap is what decides.
func TestOnlyTheWinningSwapCompletes(t *testing.T) {
	r := newRig(t)
	r.status.swapOK = false
	_ = r.runner.SubscribeToCampaign("camp")
	r.runner.SetCounter("camp", 1)

	r.sub.deliver(t, r.topic("camp"), Message{CampaignID: "camp", EntryID: "e1"})

	if len(r.sub.deleted) != 0 {
		t.Fatal("a losing replica deleted the queue")
	}
}

// ---------------------------------------------------------------- holds

// A paused campaign requeues rather than sends, and does NOT consume the
// handler: the pause has to hold even on a replica the operator never touched.
func TestPausedCampaignRequeuesWithoutHandling(t *testing.T) {
	r := newRig(t)
	_ = r.runner.SubscribeToCampaign("camp")
	r.runner.SetCounter("camp", 3)
	if err := r.runner.PauseCampaignConsumer("camp"); err != nil {
		t.Fatal(err)
	}

	ack := r.sub.deliver(t, r.topic("camp"), Message{CampaignID: "camp", EntryID: "e1"})

	if r.calls != 0 {
		t.Fatal("the send step ran while the campaign was paused")
	}
	if !ack.acked || len(r.pub.delayed) != 1 {
		t.Fatal("the paused message was not requeued")
	}
	if remaining, _ := r.shared.GetString(testNS.RemainingKey("camp")); remaining != "3" {
		t.Fatalf("a paused message was counted: remaining = %s", remaining)
	}
}

// A stopped campaign discards work already read off the queue. The queue is
// deleted on stop, but a message in flight is no longer in it.
func TestStoppedCampaignDiscardsInFlightWork(t *testing.T) {
	r := newRig(t)
	_ = r.runner.SubscribeToCampaign("camp")
	handler := r.sub.handlers[r.topic("camp")]
	_ = r.runner.StopCampaignConsumer("camp")

	raw, _ := json.Marshal(Message{CampaignID: "camp", EntryID: "e1"})
	ack := &fakeAck{}
	handler(raw, ack)

	if r.calls != 0 {
		t.Fatal("a stopped campaign still sent")
	}
	if !ack.acked {
		t.Fatal("the discarded message was not acked")
	}
}

// Stopping clears the counter, so restarting cannot inherit a stale one.
func TestStopClearsCoordinationState(t *testing.T) {
	r := newRig(t)
	_ = r.runner.SubscribeToCampaign("camp")
	r.runner.SetCounter("camp", 5)
	_ = r.runner.PauseCampaignConsumer("camp")
	_ = r.runner.StopCampaignConsumer("camp")

	if exists, _ := r.shared.Exists(testNS.RemainingKey("camp")); exists {
		t.Error("the completion counter survived a stop")
	}
	if exists, _ := r.shared.Exists(testNS.PausedKey("camp")); exists {
		t.Error("the pause flag survived a stop")
	}
	if exists, _ := r.shared.Exists(testNS.StoppedKey("camp")); !exists {
		t.Error("the stop flag was not set")
	}
}

// Starting a campaign that was previously stopped must clear the stop flag, or
// every message it takes is silently discarded.
func TestSubscribeClearsHeldFlags(t *testing.T) {
	r := newRig(t)
	_ = r.shared.SetString(testNS.StoppedKey("camp"), "1", 0)
	_ = r.shared.SetString(testNS.PausedKey("camp"), "1", 0)

	if err := r.runner.SubscribeToCampaign("camp"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{testNS.StoppedKey("camp"), testNS.PausedKey("camp")} {
		if exists, _ := r.shared.Exists(key); exists {
			t.Errorf("%s survived a subscribe", key)
		}
	}
}

// The flags clear even when the channel then refuses the campaign, so a
// rejected one does not carry a stop flag into its next successful start.
func TestFlagsClearEvenWhenPrecheckRefuses(t *testing.T) {
	r := newRig(t)
	refused := errors.New("template not approved")
	r.runner.cfg.Precheck = func(string) error { return refused }
	_ = r.shared.SetString(testNS.StoppedKey("camp"), "1", 0)

	if err := r.runner.SubscribeToCampaign("camp"); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the precheck's", err)
	}
	if exists, _ := r.shared.Exists(testNS.StoppedKey("camp")); exists {
		t.Fatal("a refused campaign kept its stop flag")
	}
	if r.runner.IsSubscribed("camp") {
		t.Fatal("a refused campaign was subscribed anyway")
	}
}

// ---------------------------------------------------------------- boot

// A campaign whose queue drained during a restart is COMPLETED, not
// resubscribed: completion is counted per message, and there are no more.
func TestStartCompletesCampaignsWithAnEmptyQueue(t *testing.T) {
	r := newRig(t)
	r.status.running = []string{"drained"}

	if err := r.runner.Start(); err != nil {
		t.Fatal(err)
	}
	if len(r.status.completed) != 1 {
		t.Fatalf("completed %d, want 1", len(r.status.completed))
	}
	if r.runner.IsSubscribed("drained") {
		t.Fatal("a drained campaign was resubscribed")
	}
}

// A campaign with work is resubscribed and its counter rebuilt from the
// database, because the counter has a TTL and lives in a cache that can be
// flushed.
func TestStartResubscribesAndSeedsTheCounter(t *testing.T) {
	r := newRig(t)
	r.status.running = []string{"live"}
	r.sub.lengths[r.topic("live")] = 4
	r.pending.n = 4

	if err := r.runner.Start(); err != nil {
		t.Fatal(err)
	}
	if !r.runner.IsSubscribed("live") {
		t.Fatal("a live campaign was not resubscribed")
	}
	if got, _ := r.shared.GetString(testNS.RemainingKey("live")); got != "4" {
		t.Fatalf("counter = %q, want 4", got)
	}
}

// Work waiting on a retry lives in the DELAY queue. A campaign whose main queue
// is empty but whose delay queue is not is very much unfinished.
func TestStartTreatsTheDelayQueueAsWork(t *testing.T) {
	r := newRig(t)
	r.status.running = []string{"waiting"}
	r.sub.lengths[r.topic("waiting")+messaging.DelayQueueSuffix] = 2
	r.pending.n = 2

	if err := r.runner.Start(); err != nil {
		t.Fatal(err)
	}
	if len(r.status.completed) != 0 {
		t.Fatal("a campaign with delayed work was completed")
	}
}

func TestStartRequiresASubscriber(t *testing.T) {
	r := newRig(t)
	r.runner.sub = nil
	if err := r.runner.Start(); err == nil {
		t.Fatal("Start accepted a nil subscriber")
	}
}

// ---------------------------------------------------------------- messages

// A message that cannot decode will never decode. Requeuing spins forever.
func TestUndecodableMessageIsDroppedWithoutRequeue(t *testing.T) {
	r := newRig(t)
	_ = r.runner.SubscribeToCampaign("camp")

	ack := &fakeAck{}
	r.sub.handlers[r.topic("camp")]([]byte("{not json"), ack)

	if !ack.nacked || ack.nackRequeue {
		t.Fatalf("nacked=%v requeue=%v, want nacked without requeue", ack.nacked, ack.nackRequeue)
	}
	if r.calls != 0 {
		t.Fatal("the send step ran on an undecodable message")
	}
}

func TestSubscribingTwiceIsANoOp(t *testing.T) {
	r := newRig(t)
	_ = r.runner.SubscribeToCampaign("camp")
	if err := r.runner.SubscribeToCampaign("camp"); err != nil {
		t.Fatal(err)
	}
	if !r.runner.IsSubscribed("camp") {
		t.Fatal("the campaign lost its subscription")
	}
}

// ---------------------------------------------------------------- dispatcher

// The counter is armed AFTER publishing: setting it first and then failing to
// publish leaves a campaign that can never reach zero.
func TestDispatcherArmsTheCounterOnlyAfterPublishing(t *testing.T) {
	pub := &fakePub{err: errors.New("broker down")}
	sharedState := newMemShared()
	d := NewDispatcher(pub, sharedState, testNS)

	err := d.Enqueue("camp", []Message{{CampaignID: "camp", EntryID: "e1"}})
	if err == nil {
		t.Fatal("the publish failure did not surface")
	}
	if exists, _ := sharedState.Exists(testNS.RemainingKey("camp")); exists {
		t.Fatal("the counter was armed despite a failed publish")
	}
}

func TestDispatcherPublishesOnePerEntry(t *testing.T) {
	pub := &fakePub{}
	sharedState := newMemShared()
	d := NewDispatcher(pub, sharedState, testNS)

	msgs := []Message{
		{CampaignID: "camp", EntryID: "e1"},
		{CampaignID: "camp", EntryID: "e2"},
		{CampaignID: "camp", EntryID: "e3"},
	}
	if err := d.Enqueue("camp", msgs); err != nil {
		t.Fatal(err)
	}
	if pub.published != 3 {
		t.Fatalf("published %d, want 3", pub.published)
	}
	if got, _ := sharedState.GetString(testNS.RemainingKey("camp")); got != "3" {
		t.Fatalf("counter = %q, want 3", got)
	}
}

func TestDispatcherIgnoresAnEmptyBatch(t *testing.T) {
	pub := &fakePub{}
	sharedState := newMemShared()
	if err := NewDispatcher(pub, sharedState, testNS).Enqueue("camp", nil); err != nil {
		t.Fatal(err)
	}
	if exists, _ := sharedState.Exists(testNS.RemainingKey("camp")); exists {
		t.Fatal("an empty dispatch armed the counter")
	}
}

// Two channels' campaigns must never share a key, whatever their ids.
func TestNamespacesDoNotCollide(t *testing.T) {
	a := campaign.Namespace{Topic: "wa", Key: "campaign:whatsapp"}
	b := campaign.Namespace{Topic: "uw", Key: "campaign:unofficial_whatsapp"}

	for _, pair := range [][2]string{
		{a.PausedKey("x"), b.PausedKey("x")},
		{a.StoppedKey("x"), b.StoppedKey("x")},
		{a.RemainingKey("x"), b.RemainingKey("x")},
		{a.DispatchTopic("x"), b.DispatchTopic("x")},
		{a.QuickSendLockKey("x"), b.QuickSendLockKey("x")},
	} {
		if pair[0] == pair[1] {
			t.Errorf("two channels collided on %q", pair[0])
		}
	}
}
