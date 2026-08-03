package crm_telemetry_usecase

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
)

// recordingPublisher counts published events so we can assert the emitter drops
// malformed ones before they ever reach the queue.
type recordingPublisher struct{ published int }

func (r *recordingPublisher) Publish(kind crm_telemetry.Kind, payload any) error {
	r.published++
	return nil
}

// The emitter is the single choke point: a conversation event with an empty
// workspace_id must be dropped, not published (it can never be persisted and
// would poison the consumer). Regression for the analysis_created flood.
func TestEmitter_ConversationEvent_DropsEmptyWorkspace(t *testing.T) {
	rp := &recordingPublisher{}
	e := NewEmitter(rp)

	e.ConversationEvent(ce.New("", "e1", "whatsapp", ce.EventAnalysisCreated).Build())
	if rp.published != 0 {
		t.Fatalf("empty-workspace event must be dropped, published=%d", rp.published)
	}

	e.ConversationEvent(ce.New("ws1", "e1", "whatsapp", ce.EventAnalysisCreated).Build())
	if rp.published != 1 {
		t.Fatalf("valid event must publish, published=%d", rp.published)
	}
}

// AnalysisCreated (the exact producer that flooded prod) routes through the same
// choke point, so an empty workspace never publishes.
func TestEmitter_AnalysisCreated_EmptyWorkspaceDropped(t *testing.T) {
	rp := &recordingPublisher{}
	e := NewEmitter(rp)
	e.AnalysisCreated("", "e1", "whatsapp", "analysis-1", "pending", 0)
	if rp.published != 0 {
		t.Fatalf("AnalysisCreated with empty workspace must not publish, published=%d", rp.published)
	}
	e.AnalysisCreated("ws1", "e1", "whatsapp", "analysis-1", "pending", 90)
	if rp.published != 1 {
		t.Fatalf("valid AnalysisCreated must publish, published=%d", rp.published)
	}
}

// If a malformed event is already in the queue (e.g. the 217k backlog published
// before this fix), the consumer must DROP it (Nack without requeue), never loop.
func TestConsumer_EmptyWorkspace_DroppedNotRequeued(t *testing.T) {
	evRepo := &memEvents{}
	c := NewConsumerWithDeps(ConsumerDeps{
		Events:    evRepo,
		History:   &memHistory{},
		AIRepo:    newMemAI(),
		QueueRepo: &memQueue{},
		Presence:  &memPresence{},
		Dedupe:    &memDedupe{},
	}).(*consumer)

	ev := ce.New("", "0dc89eb0-264c-4aab-987e-b4ce4a1c0f5e", "whatsapp", ce.EventAnalysisCreated).Build()
	ev.ID = "poison-1"
	body, _ := json.Marshal(ev)
	env := crm_telemetry.Envelope{ID: "poison-1", Kind: crm_telemetry.KindConversationEvent, Payload: body}
	raw, _ := json.Marshal(env)

	ack := &fakeAck{count: 1}
	c.handle(raw, ack)
	time.Sleep(50 * time.Millisecond)

	if ack.requeue {
		t.Fatal("poison event must NOT be requeued, that is the infinite loop")
	}
	if !ack.nacked {
		t.Fatal("poison event must be nacked (dropped)")
	}
	if len(evRepo.all) != 0 {
		t.Fatalf("poison event must not be persisted, got %d", len(evRepo.all))
	}
}

func TestIsPermanentError(t *testing.T) {
	permanent := []error{
		ce.ErrInvalidEvent,
		errors.New(`ERROR: invalid input syntax for type uuid: "" (SQLSTATE 22P02)`),
		errors.New(`null value in column violates not-null constraint (SQLSTATE 23502)`),
		errors.New(`duplicate key value violates unique constraint (SQLSTATE 23505)`),
	}
	for _, err := range permanent {
		if !isPermanentError(err) {
			t.Errorf("expected permanent (drop): %v", err)
		}
	}
	transient := []error{
		nil,
		errors.New("dial tcp 127.0.0.1:5432: connection refused"),
		errors.New(`deadlock detected (SQLSTATE 40P01)`),
		errors.New("context deadline exceeded"),
	}
	for _, err := range transient {
		if isPermanentError(err) {
			t.Errorf("expected transient (requeue): %v", err)
		}
	}
}
