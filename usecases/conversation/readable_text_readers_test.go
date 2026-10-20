package conversation_usecase

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func spokenAndRetracted() []*conversation.Message {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	return []*conversation.Message{
		{ID: "a", MessageType: conversation.MessageTypeAudio, Direction: conversation.MessageDirectionInbound, Text: conversation.AudioPlaceholder, Transcript: "quero o plano anual"},
		{ID: "b", MessageType: conversation.MessageTypeUserMessage, Direction: conversation.MessageDirectionInbound, Text: "segredo apagado", RetractedAt: &at},
		{ID: "c", MessageType: conversation.MessageTypeOperator, Direction: conversation.MessageDirectionOutbound, Text: "claro"},
	}
}

func TestTheAnalysisTranscriptHearsTheAudioAndSkipsRetractedMessages(t *testing.T) {
	transcript := BuildTranscript(spokenAndRetracted())
	if !strings.Contains(transcript, conversation.AudioPlaceholder+" quero o plano anual") {
		t.Fatalf("the transcript lost the audio: %q", transcript)
	}
	if strings.Contains(transcript, "segredo") {
		t.Fatalf("a retracted message reached the analysis: %q", transcript)
	}
}

func TestTheChannelAIReplyHistoryHearsTheAudioAndSkipsRetractedMessages(t *testing.T) {
	newestFirst := spokenAndRetracted()
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	prompt := promptFrom(newestFirst, "claro", conversation.SourceLabels{})
	var contents []string
	for _, m := range prompt {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "|")
	if !strings.Contains(joined, conversation.AudioPlaceholder+" quero o plano anual") || strings.Contains(joined, "segredo") {
		t.Fatalf("prompt = %q", joined)
	}
}

func TestTheOfficialAIHistoryHearsTheAudioAndSkipsRetractedMessages(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	history := uc.composeConversationHistory(spokenAndRetracted(), "551140001234", "5511999990000")
	var contents []string
	for _, m := range history {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "|")
	if !strings.Contains(joined, conversation.AudioPlaceholder+" quero o plano anual") || strings.Contains(joined, "segredo") {
		t.Fatalf("history = %q", joined)
	}
}
