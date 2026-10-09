package coexistence_usecase

import (
	"testing"

	"vozko/domain/coexistence"
	"vozko/domain/conversation"
	"vozko/domain/geo"
	businessphone "vozko/domain/whatsapp/business_phone"
)

func TestACoexistenceLocationIsReadBackAsALocationCandidate(t *testing.T) {
	businessPhone := &businessphone.WhatsAppBusinessPhoneNumber{DisplayPhoneNumber: "5511999999999"}
	place := &coexistence.HistoryLocation{Latitude: -23.55052, Longitude: -46.633308, Name: "Casa", Address: "Rua A, 10"}
	cases := []struct {
		name      string
		from      string
		candidate bool
	}{
		{"sent by the lead", "5511888888888", true},
		{"sent by the business", "5511999999999", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgType, _, text := classifyHistoryMessage(coexistence.HistoryMessage{From: tc.from, Type: "location", Location: place}, businessPhone)
			stored := &conversation.Message{ID: "msg-1", Channel: conversation.MessageChannelWhatsApp, MessageType: msgType, Text: text}
			found, ok := conversation.LocationOf(stored)
			if !ok || found.Point != (geo.Point{Lat: -23.55052, Lng: -46.633308}) {
				t.Fatalf("LocationOf(%q) = %+v, %v", text, found, ok)
			}
			if _, ok := conversation.LocationCandidateOf(stored); ok != tc.candidate {
				t.Fatalf("candidate = %v, want %v", ok, tc.candidate)
			}
		})
	}
}
