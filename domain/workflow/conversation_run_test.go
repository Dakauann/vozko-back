package workflow

import (
	"testing"
	"time"
)

func TestOnlyACallRunHasNoConversationToGuard(t *testing.T) {
	cases := []struct {
		entryType string
		want      bool
	}{
		{"whatsapp", true},
		{"instagram", true},
		{"unknown", true},
		{"", true},
		{EntryTypeSIPCall, false},
	}
	for _, tc := range cases {
		run := &WorkflowRun{EntryType: tc.entryType}
		if got := run.OnConversation(); got != tc.want {
			t.Fatalf("%q: OnConversation = %v, want %v", tc.entryType, got, tc.want)
		}
	}
}

func TestARunLastsFromItsStartToItsEndOnceItHasEnded(t *testing.T) {
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	end := start.Add(1500 * time.Millisecond)
	before := start.Add(-time.Second)
	cases := []struct {
		name      string
		status    RunStatus
		completed *time.Time
		want      *int64
	}{
		{"completed", RunStatusCompleted, &end, ms(1500)},
		{"failed", RunStatusError, &end, ms(1500)},
		{"cancelled", RunStatusCancelled, &end, ms(1500)},
		{"still running has no duration yet", RunStatusRunning, &end, nil},
		{"waiting has no duration yet", RunStatusWaiting, nil, nil},
		{"ended without an end time", RunStatusCompleted, nil, nil},
		{"a clock skew never reads as negative", RunStatusCompleted, &before, ms(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConversationRun{Status: tc.status, StartedAt: start, CompletedAt: tc.completed}.Duration()
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("duration = %v, want %v", deref(got), deref(tc.want))
			}
		})
	}
}

func TestOnlyALiveRunNamesTheStepItIsOn(t *testing.T) {
	names := stepNames{"action_send_message": "Enviar mensagem"}
	live := ConversationRun{Status: RunStatusWaiting, CurrentNodeType: "action_send_message"}.Described(names)
	if live.CurrentNodeName != "Enviar mensagem" {
		t.Fatalf("live run step = %q", live.CurrentNodeName)
	}
	ended := ConversationRun{Status: RunStatusCompleted, CurrentNodeID: "n1", CurrentNodeType: "action_send_message"}.Described(names)
	if ended.CurrentNodeName != "" || ended.CurrentNodeID != "" || ended.CurrentNodeType != "" {
		t.Fatalf("an ended run must not point at a step: %+v", ended)
	}
	unknown := ConversationRun{Status: RunStatusRunning}.Described(names)
	if unknown.CurrentNodeName != "" {
		t.Fatalf("a step that cannot be read has no name: %+v", unknown)
	}
}

type stepNames map[NodeType]string

func (s stepNames) StepName(nodeType NodeType) string { return s[nodeType] }

func ms(v int64) *int64 { return &v }

func deref(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
