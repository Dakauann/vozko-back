package conversation

import (
	"reflect"
	"testing"
)

func sentByWorkflowMessage(workflowID, text string) *Message {
	return &Message{Text: text, SentBy: SentByWorkflow(workflowID)}
}

func TestTheWorkflowsBehindAHistoryAreListedOnce(t *testing.T) {
	history := []*Message{
		sentByWorkflowMessage("wf-1", "a"),
		nil,
		{Text: "oi", SentBy: SentByContact("5511")},
		sentByWorkflowMessage("wf-2", "b"),
		sentByWorkflowMessage("wf-1", "c"),
		{Text: "resposta", SentBy: SentByAI("agent-1")},
	}
	if got := WorkflowSenders(history); !reflect.DeepEqual(got, []string{"workflow:wf-1", "workflow:wf-2"}) {
		t.Fatalf("senders = %v", got)
	}
	if got := WorkflowSenders([]*Message{{Text: "oi", SentBy: SentByContact("5511")}}); len(got) != 0 {
		t.Fatalf("senders = %v, want none", got)
	}
}

func TestAWorkflowMessageReachesTheAgentLabelledWithItsSource(t *testing.T) {
	labels := NewSourceLabels(map[string]string{"workflow:wf-1": "Matrícula", "ai:agent-1": "Bia"})
	cases := []struct {
		name    string
		message *Message
		content string
		want    string
	}{
		{"a named workflow", sentByWorkflowMessage("wf-1", "x"), "Seu CPF?", "[fluxo Matrícula] Seu CPF?"},
		{"a workflow whose name is unknown", sentByWorkflowMessage("wf-9", "x"), "Seu CPF?", "[fluxo] Seu CPF?"},
		{"the agent's own message", &Message{SentBy: SentByAI("agent-1")}, "Olá!", "Olá!"},
		{"the customer's message", &Message{SentBy: SentByContact("5511")}, "oi", "oi"},
		{"an empty workflow message", sentByWorkflowMessage("wf-1", ""), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := labels.Label(tc.message, tc.content); got != tc.want {
				t.Fatalf("label = %q, want %q", got, tc.want)
			}
		})
	}
	if got := (SourceLabels{}).Label(sentByWorkflowMessage("wf-1", "x"), "oi"); got != "[fluxo] oi" {
		t.Fatalf("zero labels = %q", got)
	}
}
