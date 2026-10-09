package conversation

import (
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/geo"
)

const (
	saoPauloLat = -23.550520
	saoPauloLng = -46.633308
)

func inboundMessage(channel MessageChannel, text string, metadata string) *Message {
	m := &Message{ID: "msg-1", Channel: channel, MessageType: MessageTypeUserMessage, SentBy: SentByContact("5511999990000"), Text: text}
	if metadata != "" {
		m.Metadata = json.RawMessage(metadata)
	}
	return m
}

func TestLocationOfReadsEveryStoredShape(t *testing.T) {
	official := (&WhatsAppLocation{Latitude: saoPauloLat, Longitude: saoPauloLng, Name: "Casa", Address: "Rua A, 10"})
	cases := []struct {
		name    string
		message *Message
		want    Location
		ok      bool
	}{
		{"official metadata", inboundMessage(MessageChannelWhatsApp, official.Text(), string(official.Metadata())),
			Location{Point: geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}, Name: "Casa", Address: "Rua A, 10"}, true},
		{"telegram metadata", inboundMessage(MessageChannelTelegram, "[location]", `{"telegram_message_id":1,"telegram_latitude":-23.55052,"telegram_longitude":-46.633308}`),
			Location{Point: geo.Point{Lat: -23.55052, Lng: -46.633308}}, true},
		{"coexistence text", inboundMessage(MessageChannelWhatsApp, LocationText("Casa", "Rua A, 10", geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}), ""),
			Location{Point: geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}, Address: "Casa, Rua A, 10"}, true},
		{"coexistence text without name", inboundMessage(MessageChannelWhatsApp, LocationText("", "", geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}), ""),
			Location{Point: geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}}, true},
		{"plain text", inboundMessage(MessageChannelWhatsApp, "moro perto da praça (perto, mesmo)", ""), Location{}, false},
		{"text format on another channel", inboundMessage(MessageChannelInstagram, LocationText("Casa", "", geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}), ""), Location{}, false},
		{"telegram keys on whatsapp", inboundMessage(MessageChannelWhatsApp, "oi", `{"telegram_latitude":-23.5,"telegram_longitude":-46.6}`), Location{}, false},
		{"zero point", inboundMessage(MessageChannelTelegram, "[location]", `{"telegram_latitude":0,"telegram_longitude":0}`), Location{}, false},
		{"out of range", inboundMessage(MessageChannelWhatsApp, "", `{"location":{"latitude":123,"longitude":-46.6}}`), Location{}, false},
		{"latitude only", inboundMessage(MessageChannelWhatsApp, "", `{"location":{"latitude":-23.5}}`), Location{}, false},
		{"coordinates as text", inboundMessage(MessageChannelWhatsApp, "", `{"location":{"latitude":"-23.5","longitude":"-46.6"}}`), Location{}, false},
		{"broken metadata", inboundMessage(MessageChannelWhatsApp, "", `{"location":`), Location{}, false},
		{"no metadata no text", inboundMessage(MessageChannelWhatsApp, "", ""), Location{}, false},
		{"nil message", nil, Location{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LocationOf(tc.message)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("LocationOf() = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLocationCandidateOfOnlyOffersWhatTheLeadSentFromBrazil(t *testing.T) {
	inside := (&WhatsAppLocation{Latitude: saoPauloLat, Longitude: saoPauloLng}).Metadata()
	lisbon := (&WhatsAppLocation{Latitude: 38.7223, Longitude: -9.1393}).Metadata()
	operator := inboundMessage(MessageChannelWhatsApp, "", string(inside))
	operator.SentBy, operator.MessageType = SentByPerson("user-1"), MessageTypeOperator
	coexistenceEcho := inboundMessage(MessageChannelWhatsApp, LocationText("Loja", "", geo.Point{Lat: saoPauloLat, Lng: saoPauloLng}), "")
	coexistenceEcho.SentBy, coexistenceEcho.MessageType = SentBy{}, MessageTypeOperator
	cases := []struct {
		name    string
		message *Message
		ok      bool
	}{
		{"inbound in Brazil", inboundMessage(MessageChannelWhatsApp, "", string(inside)), true},
		{"inbound abroad", inboundMessage(MessageChannelWhatsApp, "", string(lisbon)), false},
		{"sent by an operator", operator, false},
		{"coexistence echo of the business", coexistenceEcho, false},
		{"not a location", inboundMessage(MessageChannelWhatsApp, "oi", ""), false},
		{"inbound without an id", &Message{Channel: MessageChannelWhatsApp, MessageType: MessageTypeUserMessage, Metadata: inside}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LocationCandidateOf(tc.message)
			if ok != tc.ok {
				t.Fatalf("LocationCandidateOf() ok = %v, want %v (%+v)", ok, tc.ok, got)
			}
			if ok && (got.MessageID != tc.message.ID || got.Channel != tc.message.Channel || got.Point != (geo.Point{Lat: saoPauloLat, Lng: saoPauloLng})) {
				t.Fatalf("LocationCandidateOf() = %+v", got)
			}
		})
	}
}

func TestLocationTextKeepsTheStoredCoexistenceFormat(t *testing.T) {
	got := LocationText("Casa", "Rua A, 10", geo.Point{Lat: saoPauloLat, Lng: saoPauloLng})
	if want := "📍 Casa, Rua A, 10 (-23.550520, -46.633308)"; got != want {
		t.Fatalf("LocationText() = %q, want %q", got, want)
	}
}

func TestWhatsAppLocationMetadataCarriesTheCoordinates(t *testing.T) {
	var nilLocation *WhatsAppLocation
	if nilLocation.Metadata() != nil || nilLocation.Text() != "" {
		t.Fatal("a message without a location carries no location metadata or text")
	}
	raw := (&WhatsAppLocation{Latitude: saoPauloLat, Longitude: saoPauloLng, Name: "Casa"}).Metadata()
	if want := `{"location":{"latitude":-23.55052,"longitude":-46.633308,"name":"Casa"}}`; string(raw) != want {
		t.Fatalf("metadata = %s, want %s", raw, want)
	}
}

func TestMessageJSONExposesItsLocation(t *testing.T) {
	inside := (&WhatsAppLocation{Latitude: saoPauloLat, Longitude: saoPauloLng, Name: "Casa", Address: "Rua A"}).Metadata()
	operator := inboundMessage(MessageChannelWhatsApp, "", string(inside))
	operator.SentBy, operator.MessageType = SentByPerson("user-1"), MessageTypeOperator
	cases := []struct {
		name    string
		message *Message
		want    string
	}{
		{"candidate", inboundMessage(MessageChannelWhatsApp, "📍", string(inside)),
			`"location":{"latitude":-23.55052,"longitude":-46.633308,"name":"Casa","address":"Rua A","candidate":true}`},
		{"shown but not offered", operator,
			`"location":{"latitude":-23.55052,"longitude":-46.633308,"name":"Casa","address":"Rua A","candidate":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.message)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tc.want) || !strings.Contains(string(raw), `"id":"msg-1"`) {
				t.Fatalf("json = %s", raw)
			}
		})
	}
	raw, err := json.Marshal(inboundMessage(MessageChannelWhatsApp, "oi", ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"location"`) {
		t.Fatalf("a plain message has no location: %s", raw)
	}
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil || back.ID != "msg-1" || back.Text != "oi" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}
