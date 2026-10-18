package consentevidence

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

var (
	here       = Conversation{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}
	askedAt    = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	answeredAt = askedAt.Add(time.Minute)
)

func leadSays(id, text string, at time.Time) *conversation.Message {
	return &conversation.Message{
		ID: id, EntryID: here.EntryID, EntryType: here.EntryType, Text: text,
		MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511999990000"), CreatedAt: at,
	}
}

func agentAsks(agentID, text string, at time.Time) *conversation.Message {
	return &conversation.Message{
		ID: "question", EntryID: here.EntryID, EntryType: here.EntryType, Text: text,
		MessageType: conversation.MessageTypeAIResponse, SentBy: conversation.SentByAI(agentID), CreatedAt: at,
	}
}

func elsewhere(m *conversation.Message) *conversation.Message {
	moved := *m
	moved.EntryID = "entry-2"
	return &moved
}

func TestALeadMessageInThisConversationIsEvidence(t *testing.T) {
	evidence, err := LeadMessage(here, leadSays("m-1", "  não quero mais receber isso  ", answeredAt))
	if err != nil {
		t.Fatal(err)
	}
	want := lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-1", Excerpt: "não quero mais receber isso"}
	if evidence != want {
		t.Fatalf("evidence = %+v, want %+v", evidence, want)
	}
}

func TestTheExcerptKeepsTheLeadsWordsWithinTheLimit(t *testing.T) {
	long := strings.Repeat("ã", lead.MaxConsentExcerptLength+40)
	evidence, err := LeadMessage(here, leadSays("m-1", long, answeredAt))
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(evidence.Excerpt)); got != lead.MaxConsentExcerptLength {
		t.Fatalf("excerpt has %d runes, want %d", got, lead.MaxConsentExcerptLength)
	}
}

func TestOnlyTheLeadsOwnMessageInThisConversationIsEvidence(t *testing.T) {
	cases := []struct {
		name    string
		message *conversation.Message
		want    error
	}{
		{"an unknown message", nil, ErrMessageUnknown},
		{"another conversation", elsewhere(leadSays("m-1", "pare", answeredAt)), ErrOutsideConversation},
		{"another channel with the same id", func() *conversation.Message {
			m := leadSays("m-1", "pare", answeredAt)
			m.EntryType = shared.EntryTypeInstagram
			return m
		}(), ErrOutsideConversation},
		{"the agent's own message", agentAsks("agent-1", "posso parar?", askedAt), ErrNotFromLead},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LeadMessage(here, tc.message)
			if !errors.Is(err, tc.want) || !errors.Is(err, lead.ErrConsentEvidenceInvalid) {
				t.Fatalf("err = %v, want %v coded as lead_consent_evidence_invalid", err, tc.want)
			}
		})
	}
}

func TestAYesToTheAgentsOwnQuestionIsEvidence(t *testing.T) {
	evidence, err := AnswerToAgentQuestion(here, "agent-1",
		agentAsks("agent-1", "Posso te mandar novidades por aqui?", askedAt),
		leadSays("m-2", "pode sim", answeredAt))
	if err != nil {
		t.Fatal(err)
	}
	want := lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-2", Excerpt: "pode sim"}
	if evidence != want {
		t.Fatalf("evidence = %+v, want %+v", evidence, want)
	}
}

func TestAConsentNeedsTheAgentsOwnEarlierQuestion(t *testing.T) {
	human := agentAsks("agent-1", "Posso te mandar novidades?", askedAt)
	human.SentBy = conversation.SentByPerson("user-9")
	cases := []struct {
		name     string
		question *conversation.Message
		answer   *conversation.Message
		want     error
	}{
		{"no question", nil, leadSays("m-2", "sim", answeredAt), ErrQuestionUnknown},
		{"a question from another agent", agentAsks("agent-2", "Posso?", askedAt), leadSays("m-2", "sim", answeredAt), ErrNotTheAgentsQuestion},
		{"a question from a person", human, leadSays("m-2", "sim", answeredAt), ErrNotTheAgentsQuestion},
		{"the lead's own message as the question", leadSays("m-1", "posso receber?", askedAt), leadSays("m-2", "sim", answeredAt), ErrNotTheAgentsQuestion},
		{"a question in another conversation", elsewhere(agentAsks("agent-1", "Posso?", askedAt)), leadSays("m-2", "sim", answeredAt), ErrOutsideConversation},
		{"an answer before the question", agentAsks("agent-1", "Posso?", answeredAt), leadSays("m-2", "sim", askedAt), ErrAnswerBeforeQuestion},
		{"an answer at the same instant", agentAsks("agent-1", "Posso?", askedAt), leadSays("m-2", "sim", askedAt), ErrAnswerBeforeQuestion},
		{"an answer that is not the lead's", agentAsks("agent-1", "Posso?", askedAt), agentAsks("agent-1", "sim", answeredAt), ErrNotFromLead},
		{"an answer in another conversation", agentAsks("agent-1", "Posso?", askedAt), elsewhere(leadSays("m-2", "sim", answeredAt)), ErrOutsideConversation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AnswerToAgentQuestion(here, "agent-1", tc.question, tc.answer)
			if !errors.Is(err, tc.want) || !errors.Is(err, lead.ErrConsentEvidenceInvalid) {
				t.Fatalf("err = %v, want %v coded as lead_consent_evidence_invalid", err, tc.want)
			}
		})
	}
}

func TestAnAgentWithoutAnIDCannotCiteItsOwnQuestion(t *testing.T) {
	_, err := AnswerToAgentQuestion(here, " ", agentAsks("", "Posso?", askedAt), leadSays("m-2", "sim", answeredAt))
	if !errors.Is(err, ErrNotTheAgentsQuestion) {
		t.Fatalf("err = %v, want %v", err, ErrNotTheAgentsQuestion)
	}
}
