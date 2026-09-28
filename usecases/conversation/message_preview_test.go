package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
)

func TestStoryEventsGetAReadablePreview(t *testing.T) {
	s := &HistoryProviderService{}
	cases := map[conversation.MessageType]string{
		conversation.MessageTypeStoryMention: "[menção em story]",
		conversation.MessageTypeStoryReply:   "[resposta a story]",
	}
	for messageType, want := range cases {
		if got := s.formatMessagePreview(conversation.EntryWithLastMessage{LastMessageType: messageType}); got != want {
			t.Errorf("%s: got %q, want %q", messageType, got, want)
		}
	}
	reply := conversation.EntryWithLastMessage{LastMessageType: conversation.MessageTypeStoryReply, LastMessageText: "que lindo"}
	if got := s.formatMessagePreview(reply); got != "que lindo" {
		t.Errorf("a story reply with text previews the text, got %q", got)
	}
}
