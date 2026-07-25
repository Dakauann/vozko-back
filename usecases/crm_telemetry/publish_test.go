package crm_telemetry_usecase

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/crm_telemetry"
)

type fakePub struct {
	topic string
	body  []byte
	n     int
}

func (f *fakePub) Publish(topic string, message []byte) error {
	f.n++
	f.topic = topic
	f.body = append([]byte(nil), message...)
	return nil
}

func (f *fakePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	return f.Publish(topic, message)
}

func (f *fakePub) ValidateConnection() error { return nil }

func TestPublisher_PresenceEnvelope(t *testing.T) {
	fp := &fakePub{}
	p := NewPublisher(fp)
	_ = p.Publish(crm_telemetry.KindPresence, crm_telemetry.PresencePayload{
		WorkspaceID: "ws",
		UserID:      "u1",
		State:       "online",
		Source:      "ws_hub",
	})
	if fp.n != 1 {
		t.Fatalf("publish count=%d", fp.n)
	}
	if fp.topic != crm_telemetry.Topic {
		t.Fatalf("topic=%q", fp.topic)
	}
	var env crm_telemetry.Envelope
	if err := json.Unmarshal(fp.body, &env); err != nil {
		t.Fatal(err)
	}
	if env.Kind != crm_telemetry.KindPresence {
		t.Fatalf("kind=%q", env.Kind)
	}
	var pl crm_telemetry.PresencePayload
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	if pl.UserID != "u1" || pl.State != "online" {
		t.Fatalf("payload=%+v", pl)
	}
}

func TestPresenceAdapter_DoesNotPanic(t *testing.T) {
	fp := &fakePub{}
	a := NewPresenceAdapter(NewPublisher(fp))
	if err := a.Transition("ws", "u", "offline", "ws_hub"); err != nil {
		t.Fatal(err)
	}
	if fp.n != 1 {
		t.Fatalf("n=%d", fp.n)
	}
}
