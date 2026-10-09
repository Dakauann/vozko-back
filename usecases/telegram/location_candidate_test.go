package telegram

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/geo"
	tgdomain "vozko/domain/telegram"
)

func TestATelegramLocationIsReadBackAsALocationCandidate(t *testing.T) {
	ev := &tgdomain.Event{MessageID: 7, ChatID: 42, Location: &tgdomain.Location{Latitude: -23.55052, Longitude: -46.633308}}
	stored := &conversation.Message{
		ID: "msg-1", Channel: conversation.MessageChannelTelegram, MessageType: conversation.MessageTypeUnsupported,
		SentBy: conversation.SentByContact("42"), Text: placeholderFor(ev), Metadata: inboundMetadata(ev),
	}
	candidate, ok := conversation.LocationCandidateOf(stored)
	if !ok || candidate.Point != (geo.Point{Lat: -23.55052, Lng: -46.633308}) || candidate.Channel != conversation.MessageChannelTelegram {
		t.Fatalf("LocationCandidateOf() = %+v, %v", candidate, ok)
	}
}
