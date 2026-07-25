package conversation_event

import (
	"errors"
	"testing"

	"vozko/domain/actor"
)

func TestEventTypeValid(t *testing.T) {
	if !EventAssigned.Valid() || !EventAIReplied.Valid() || !EventStageChanged.Valid() {
		t.Fatal("expected known types valid")
	}
	if EventType("nope").Valid() {
		t.Fatal("unknown type should be invalid")
	}
}

func TestBuilderAI(t *testing.T) {
	ev := New("ws", "e1", "whatsapp", EventAIReplied).
		WithActorAI("agent-1").
		WithChannel("whatsapp").
		WithCorrelation("msg-9").
		WithDetails(map[string]string{"message_id": "msg-9"}).
		Build()
	if ev.ActorKind != actor.KindAI {
		t.Fatalf("kind=%q", ev.ActorKind)
	}
	if ev.ActorID != "ai:agent-1" {
		t.Fatalf("actor_id=%q", ev.ActorID)
	}
	if ev.Channel != "whatsapp" || ev.CorrelationID != "msg-9" {
		t.Fatalf("channel/corr missing: %+v", ev)
	}
}

func TestNormalizeFillsKind(t *testing.T) {
	ev := &ConversationEvent{ActorID: "ai:x"}
	ev.Normalize()
	if ev.ActorKind != actor.KindAI {
		t.Fatalf("kind=%q", ev.ActorKind)
	}
}

func TestDetailsJSON(t *testing.T) {
	if DetailsJSON(nil) != "" {
		t.Fatal("empty details")
	}
	s := DetailsJSON(map[string]string{"a": "b"})
	if s == "" || s[0] != '{' {
		t.Fatalf("json=%q", s)
	}
}

func TestValidate_RejectsEmptyWorkspaceOrEntry(t *testing.T) {
	cases := []struct {
		name    string
		ev      *ConversationEvent
		wantErr bool
	}{
		{"valid", &ConversationEvent{WorkspaceID: "ws-1", EntryID: "e-1", EventType: EventAnalysisCreated}, false},
		{"empty workspace (the production poison)", &ConversationEvent{WorkspaceID: "", EntryID: "e-1", EventType: EventAnalysisCreated}, true},
		{"blank workspace", &ConversationEvent{WorkspaceID: "   ", EntryID: "e-1"}, true},
		{"empty entry", &ConversationEvent{WorkspaceID: "ws-1", EntryID: "", EventType: EventReplied}, true},
		{"unknown event type", &ConversationEvent{WorkspaceID: "ws-1", EntryID: "e-1", EventType: "not_a_real_event"}, true},
		{"nil", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ev.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected ErrInvalidEvent, got nil")
				}
				if !errors.Is(err, ErrInvalidEvent) {
					t.Fatalf("expected ErrInvalidEvent, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}
