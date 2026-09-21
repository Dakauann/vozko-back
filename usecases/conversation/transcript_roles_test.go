package conversation_usecase

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
)

func TestTranscriptAttributesTurnsByDirection(t *testing.T) {
	history := []*conversation.Message{
		{Text: "oi, quanto custa?", MessageType: conversation.MessageTypeUserMessage,
			Direction: conversation.MessageDirectionInbound, From: "+558494409624"},
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
