package unofficial_whatsapp

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/messaging"
	uw "vozko/domain/unofficial_whatsapp"
)

type publishedMessage struct {
	topic   string
	payload []byte
}

type fakeQueuePub struct {
	published []publishedMessage
	// failAfter makes Publish fail once this many messages have gone out, so a
	// test can assert what a partial publish reports.
	failAfter int
	err       error
}

func (f *fakeQueuePub) Publish(topic string, message []byte) error {
	if f.err != nil && f.failAfter == len(f.published) {
		return f.err
	}
	f.published = append(f.published, publishedMessage{topic: topic, payload: message})
	return nil
}

func (f *fakeQueuePub) PublishWithDelay(topic string, message []byte, _ time.Duration) error {
	return f.Publish(topic, message)
}

func (f *fakeQueuePub) ValidateConnection() error { return nil }

func targets(n int) []uw.SeedTarget {
	out := make([]uw.SeedTarget, 0, n)
	for i := 0; i < n; i++ {
		// Distinct numbers, or Normalize deduplicates them down to one and the
		// batching is never exercised.
		out = append(out, uw.SeedTarget{Number: "55119" + pad(i)})
	}
	return out
}

func pad(i int) string {
	s := ""
	for _, d := range []int{100000, 10000, 1000, 100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	// Eight digits total with the 55119 prefix trimmed to fit minSeedPhoneDigits.
	return s + "00"
}

func TestSeedInboxPublisherSplitsAcrossBatches(t *testing.T) {
	pub := &fakeQueuePub{}
	publisher := NewSeedInboxPublisher(pub)

	all := targets(uw.SeedBatchSize + 10)
	queued, err := publisher.Publish(uw.SeedRequest{WorkspaceID: "ws-1", Targets: all})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if queued.Targets != len(all) {
		t.Fatalf("queued = %d, want %d", queued.Targets, len(all))
	}
	if len(pub.published) != 2 {
		t.Fatalf("messages published = %d, want 2", len(pub.published))
	}

	seen := 0
	for _, m := range pub.published {
		if m.topic != uw.SeedTopic {
			t.Errorf("published to %q, want %q", m.topic, uw.SeedTopic)
		}
		var batch uw.SeedRequest
		if err := json.Unmarshal(m.payload, &batch); err != nil {
			t.Fatalf("payload does not round-trip: %v", err)
		}
		if batch.WorkspaceID != "ws-1" {
			t.Errorf("batch lost the workspace: %q", batch.WorkspaceID)
		}
		seen += len(batch.Targets)
	}
	if seen != len(all) {
		t.Fatalf("batches carry %d targets, want %d", seen, len(all))
	}
}

// A request whose every row is unaddressable is not an error. Those rows were
// already reported to the operator by the import's own rejection list, and
// failing here would report them twice under a name that means something else.
func TestSeedInboxPublisherAcceptsARequestThatNormalizesToNothing(t *testing.T) {
	pub := &fakeQueuePub{}
	queued, err := NewSeedInboxPublisher(pub).Publish(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "CLIENTE"}, {Number: "12"}},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if queued.Targets != 0 {
		t.Fatalf("queued = %d, want 0", queued.Targets)
	}
	if len(pub.published) != 0 {
		t.Fatalf("published %d messages for nothing", len(pub.published))
	}
}

// A broker that fails midway has already accepted some batches, and those WILL
// seed. Reporting zero would understate what is about to appear in the inbox.
func TestSeedInboxPublisherReportsWhatItManagedToQueue(t *testing.T) {
	pub := &fakeQueuePub{failAfter: 1, err: errors.New("broker down")}
	queued, err := NewSeedInboxPublisher(pub).Publish(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     targets(uw.SeedBatchSize + 10),
	})
	if err == nil {
		t.Fatal("Publish should surface the broker failure")
	}
	if queued.Targets != uw.SeedBatchSize {
		t.Fatalf("queued = %d, want %d (the batch that got through)", queued.Targets, uw.SeedBatchSize)
	}
}

// Seeding is opt-in and the publisher is optional in the container, so a nil
// queue must be a no-op rather than a panic in the import path.
func TestSeedInboxPublisherWithoutAQueueIsANoOp(t *testing.T) {
	queued, err := NewSeedInboxPublisher(nil).Publish(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	})
	if err != nil || queued.Targets != 0 {
		t.Fatalf("Publish = (%+v, %v), want (0, nil)", queued, err)
	}
}

type fakeAck struct {
	acked    bool
	nacked   bool
	requeued bool
}

func (a *fakeAck) Ack() error { a.acked = true; return nil }
func (a *fakeAck) Nack(requeue bool) error {
	a.nacked = true
	a.requeued = requeue
	return nil
}
func (a *fakeAck) DeliveryCount() int { return 1 }

type fakeQueueSub struct{}

func (fakeQueueSub) Subscribe(string, func([]byte, messaging.MessageAck)) error { return nil }
func (fakeQueueSub) DeleteQueue(string) error                                   { return nil }
func (fakeQueueSub) ValidateConnection() error                                  { return nil }
func (fakeQueueSub) GetQueueLength(string) (int, error)                         { return 0, nil }

func TestConsumeSeedInboxAcksASeededBatch(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, _ := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)
	consumer := NewConsumeSeedInboxUseCase(fakeQueueSub{}, uc)

	payload, _ := json.Marshal(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	})
	ack := &fakeAck{}
	consumer.handle(payload, ack)

	if !ack.acked {
		t.Fatalf("batch was not acked: %+v", ack)
	}
	if len(writer.writes()) != 1 {
		t.Fatalf("placeholders written = %d, want 1", len(writer.writes()))
	}
}

// Redelivery cannot conjure a connected number, so spinning on it forever only
// costs the broker. Dropped, loudly.
func TestConsumeSeedInboxDropsWhatARetryCannotFix(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, _ := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-dead", uw.StatusDisconnected, time.Unix(100, 0))}, writer)
	consumer := NewConsumeSeedInboxUseCase(fakeQueueSub{}, uc)

	payload, _ := json.Marshal(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999"}},
	})
	ack := &fakeAck{}
	consumer.handle(payload, ack)

	if !ack.nacked || ack.requeued {
		t.Fatalf("batch should be dropped, not requeued: %+v", ack)
	}
}

func TestConsumeSeedInboxDropsAnUnreadablePayload(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _, _ := newSeedUseCase(t,
		[]*uw.Instance{seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0))}, writer)
	consumer := NewConsumeSeedInboxUseCase(fakeQueueSub{}, uc)

	ack := &fakeAck{}
	consumer.handle([]byte("{not json"), ack)

	if !ack.nacked || ack.requeued {
		t.Fatalf("a malformed payload should be dropped: %+v", ack)
	}
}

// ---- scripted batches ----

// The publisher reports two numbers because the import response has to say two
// things: how many conversations will open, and how many of those will carry a
// written thread. One number cannot say "all 500 conversations were queued, and
// 200 of them will have a script".
func TestSeedInboxPublisherReportsTheScriptedCountSeparately(t *testing.T) {
	pub := &fakeQueuePub{}
	all := targets(uw.MaxScriptedTargets + 60)

	queued, err := NewSeedInboxPublisher(pub).Publish(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     all,
		Script:      &uw.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if queued.Targets != len(all) {
		t.Errorf("queued targets = %d, want %d; nothing is dropped", queued.Targets, len(all))
	}
	// The cap, reported as the truth rather than as the ask.
	if queued.Scripted != uw.MaxScriptedTargets {
		t.Errorf("queued scripted = %d, want the %d cap", queued.Scripted, uw.MaxScriptedTargets)
	}

	scriptedTargets, plainTargets := 0, 0
	for _, m := range pub.published {
		var batch uw.SeedRequest
		if err := json.Unmarshal(m.payload, &batch); err != nil {
			t.Fatalf("payload does not round-trip: %v", err)
		}
		if batch.Script != nil {
			scriptedTargets += len(batch.Targets)
		} else {
			plainTargets += len(batch.Targets)
		}
	}
	if scriptedTargets != uw.MaxScriptedTargets {
		t.Errorf("published %d scripted targets, want %d", scriptedTargets, uw.MaxScriptedTargets)
	}
	if plainTargets != len(all)-uw.MaxScriptedTargets {
		t.Errorf("published %d plain targets, want %d", plainTargets, len(all)-uw.MaxScriptedTargets)
	}
}

// A publish that asked for no script reports zero, never nothing.
func TestSeedInboxPublisherReportsZeroScriptedWithoutAScript(t *testing.T) {
	pub := &fakeQueuePub{}
	queued, err := NewSeedInboxPublisher(pub).Publish(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     targets(10),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if queued.Targets != 10 || queued.Scripted != 0 {
		t.Fatalf("queued = %+v, want 10 targets and 0 scripted", queued)
	}
}

// A malformed script is refused before anything is queued. The handler catches
// it first; this is the second line, for a caller that did not go through it.
func TestSeedInboxPublisherRefusesAMalformedScript(t *testing.T) {
	pub := &fakeQueuePub{}
	_, err := NewSeedInboxPublisher(pub).Publish(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     targets(10),
		Script:      &uw.SeedScript{Bodies: []string{"Oi {{1}}", "Oi {{2}}"}, MaxMessages: 4},
	})
	if !errors.Is(err, uw.ErrScriptVariantMismatch) {
		t.Fatalf("Publish err = %v, want ErrScriptVariantMismatch", err)
	}
	if len(pub.published) != 0 {
		t.Fatalf("published %d messages for a malformed script", len(pub.published))
	}
}

// The consumer's context is what stands between a hung provider and a queue
// consumer that never drains again. It is applied only for a scripted batch:
// a plain batch is database work, and bounding that would only introduce a way
// for a slow import to fail.
func TestConsumeSeedInboxBoundsAScriptedBatch(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})
	consumer := NewConsumeSeedInboxUseCase(fakeQueueSub{}, uc)

	payload, _ := json.Marshal(uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999", Name: "Marina"}},
		Script:      &uw.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4},
	})
	ack := &fakeAck{}
	consumer.handle(payload, ack)

	if !ack.acked {
		t.Fatalf("a scripted batch was not acked: %+v", ack)
	}
	if len(writer.writes()) != 4 {
		t.Fatalf("wrote %d messages, want the 4-message thread", len(writer.writes()))
	}
}

// The script has to survive the queue, or a scripted batch arrives as a plain
// one and the operator's money buys nothing.
func TestSeedRequestScriptRoundTripsThroughJSON(t *testing.T) {
	original := uw.SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []uw.SeedTarget{{Number: "5511999999999", Name: "Marina"}},
		Script: &uw.SeedScript{
			Bodies:      []string{"Oi {{1}}", "Ola {{1}}"},
			MaxMessages: 6,
			Context:     "curso tecnico",
		},
	}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back uw.SeedRequest
	if err := json.Unmarshal(payload, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Script == nil {
		t.Fatal("the script did not survive the queue")
	}
	if len(back.Script.Bodies) != 2 || back.Script.MaxMessages != 6 || back.Script.Context != "curso tecnico" {
		t.Fatalf("script came back as %+v", back.Script)
	}
}
