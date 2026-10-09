package copilot

import (
	"encoding/json"
	"fmt"
	"testing"
)

func rawText(t *testing.T, ev TurnEvent) string {
	t.Helper()
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		t.Fatalf("payload %s is not a text payload: %v", ev.Payload, err)
	}
	return body.Text
}

func textPayload(text string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"text": text})
	return b
}

func TestConsecutiveDeltasReplayAsOneEvent(t *testing.T) {
	log := NewTurnLog(DefaultTurnLimits())
	log.Append(TurnEventReasoningDelta, textPayload("pen"))
	log.Append(TurnEventReasoningDelta, textPayload("sando"))
	log.Append(TurnEventAssistantDelta, textPayload("Ol"))
	log.Append(TurnEventAssistantDelta, textPayload("á"))
	log.Append("tool", json.RawMessage(`{"name":"studio_read","ok":true}`))
	log.Append(TurnEventAssistantDelta, textPayload("!"))

	events := log.Events()
	if len(events) != 4 {
		t.Fatalf("want 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != TurnEventReasoningDelta || rawText(t, events[0]) != "pensando" {
		t.Fatalf("reasoning must merge into one event, got %s %s", events[0].Type, events[0].Payload)
	}
	if events[1].Type != TurnEventAssistantDelta || rawText(t, events[1]) != "Olá" {
		t.Fatalf("text must merge into one event and stay apart from reasoning, got %s %s", events[1].Type, events[1].Payload)
	}
	if events[2].Type != "tool" || string(events[2].Payload) != `{"name":"studio_read","ok":true}` {
		t.Fatalf("other events must replay verbatim, got %s %s", events[2].Type, events[2].Payload)
	}
	if rawText(t, events[3]) != "!" {
		t.Fatalf("a delta after another event starts a new event, got %s", events[3].Payload)
	}
}

func TestTheLogKeepsTheNewestEventsWithinItsEventCap(t *testing.T) {
	limits := DefaultTurnLimits()
	limits.MaxEvents = 3
	log := NewTurnLog(limits)
	for i := 0; i < 5; i++ {
		log.Append("tool", json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)))
	}
	events := log.Events()
	if len(events) != 3 || string(events[0].Payload) != `{"n":2}` || string(events[2].Payload) != `{"n":4}` {
		t.Fatalf("the log must keep the newest 3 events, got %+v", events)
	}
	if !log.Truncated() {
		t.Fatal("a log that dropped events must say so")
	}
}

func TestTheLogKeepsTheNewestEventsWithinItsByteCap(t *testing.T) {
	limits := DefaultTurnLimits()
	limits.MaxBytes = 25
	log := NewTurnLog(limits)
	log.Append("tool", json.RawMessage(`{"name":"first_tool"}`))
	log.Append("tool", json.RawMessage(`{"name":"second"}`))
	events := log.Events()
	if len(events) != 1 || string(events[0].Payload) != `{"name":"second"}` {
		t.Fatalf("the byte cap must drop the oldest events, got %+v", events)
	}
	log.Append(TurnEventAssistantDelta, textPayload("abcdefghijklmnopqrstuvwxyz0123456789"))
	if got := log.Events(); len(got) != 1 || got[0].Type != TurnEventAssistantDelta {
		t.Fatalf("an event larger than the cap still replays alone, got %+v", got)
	}
}

func TestEventsAreACopy(t *testing.T) {
	log := NewTurnLog(DefaultTurnLimits())
	log.Append(TurnEventAssistantDelta, textPayload("a"))
	first := log.Events()
	log.Append(TurnEventAssistantDelta, textPayload("b"))
	if rawText(t, first[0]) != "a" {
		t.Fatalf("a replay already handed out must not change, got %s", first[0].Payload)
	}
}

func TestDefaultTurnLimitsAreBounded(t *testing.T) {
	l := DefaultTurnLimits()
	if l.MaxEvents <= 0 || l.MaxBytes <= 0 || l.DetachedGrace <= 0 || l.Retention <= 0 || l.SubscriberBuffer <= 0 {
		t.Fatalf("every limit must be set, got %+v", l)
	}
}
