package conversation_usecase

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
)

// Whose turn is whose, in the transcript the classifier reads.
//
// This is the single most consequential thing the renderer does. Every criterion
// in the rubric is about the exchange: did the agent answer, how did the agent
// conduct themselves, did the customer engage, did the conversation advance. A
// transcript where the business's replies are attributed to the customer is not
// a slightly worse transcript, it is a different conversation: a monologue
// nobody answered.
//
// It was inferring the role by comparing each message's sender string to the
// contact's label. On unofficial WhatsApp that comparison failed for most
// outbound messages, and a real conversation of 78 inbound / 58 outbound was
// frozen as 91 customer lines and 6 agent lines. The model read a customer
// talking to nobody and returned no_answer with a quality of zero, which is the
// correct reading of a transcript that should never have existed.
//
// The row already states the answer. Every message carries a direction, and a
// type that names the sender.
func TestTranscriptAttributesTurnsByDirection(t *testing.T) {
	history := []*conversation.Message{
		{Text: "oi, quanto custa?", MessageType: conversation.MessageTypeUserMessage,
			Direction: conversation.MessageDirectionInbound, From: "+558494409624"},
		// The operator replies from the CRM. Its sender is a user id, not a
		// phone number, which is exactly what the old comparison could not read.
		{Text: "custa R$ 300", MessageType: conversation.MessageTypeOperator,
			Direction: conversation.MessageDirectionOutbound, From: "eaed6405-8897-4056-97eb-32db4041114e"},
		{Text: "posso ajudar em mais algo?", MessageType: conversation.MessageTypeAIResponse,
			Direction: conversation.MessageDirectionOutbound, From: "agent-1"},
		{Text: "fechado", MessageType: conversation.MessageTypeUserMessage,
			Direction: conversation.MessageDirectionInbound, From: "+558494409624"},
	}

	got := BuildTranscript(history, "+558494409624")

	want := "User: oi, quanto custa?\n" +
		"Agent: custa R$ 300\n" +
		"Agent: posso ajudar em mais algo?\n" +
		"User: fechado\n"
	if got != want {
		t.Errorf("transcript attributed the wrong turns:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// The owner replying from their own phone is still the business talking. The
// direction says so even though the sender is a phone number like the
// customer's, which is the case a sender-string comparison can never get right.
func TestTranscriptTreatsAnOwnerReplyAsTheAgent(t *testing.T) {
	history := []*conversation.Message{
		{Text: "bom dia", MessageType: conversation.MessageTypeUserMessage,
			Direction: conversation.MessageDirectionInbound, From: "+558494409624"},
		{Text: "bom dia, tudo bem?", MessageType: conversation.MessageTypeOperator,
			Direction: conversation.MessageDirectionOutbound, From: "+558494409624"},
	}

	got := BuildTranscript(history, "+558494409624")

	if strings.Count(got, "Agent:") != 1 {
		t.Errorf("the owner's own reply was not attributed to the business:\n%s", got)
	}
}

// Rows written before the direction column existed still have to render. The
// message TYPE names the sender on its own, so the fallback is still a fact
// about the row rather than a guess at a phone number.
func TestTranscriptFallsBackToTheMessageTypeWithoutADirection(t *testing.T) {
	history := []*conversation.Message{
		{Text: "oi", MessageType: conversation.MessageTypeUserMessage, From: "+558494409624"},
		{Text: "olá!", MessageType: conversation.MessageTypeOperator, From: "operator"},
		{Text: "resposta da ia", MessageType: conversation.MessageTypeAIResponse, From: "agent"},
	}

	got := BuildTranscript(history, "+558494409624")

	want := "User: oi\nAgent: olá!\nAgent: resposta da ia\n"
	if got != want {
		t.Errorf("legacy rows lost their attribution:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
