package conversation

import (
	"encoding/json"
	"testing"
	"time"
)

func wireOf(t *testing.T, m *Message) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheWireCarriesReactionsInOneShapeForNewAndLegacyRows(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	stored := &Message{ID: "m1", Text: "oi"}
	if err := stored.React(Reaction{Emoji: "👍", Direction: ReactionInbound, ActorID: "5511", At: at}); err != nil {
		t.Fatal(err)
	}
	legacy := &Message{ID: "m2", Text: "oi", Metadata: json.RawMessage(`{"facebook_reaction_emoji":"😮","facebook_reaction_at":"2026-10-18T12:00:00Z"}`)}

	for name, m := range map[string]*Message{"stored": stored, "legacy": legacy} {
		reactions, ok := wireOf(t, m)["reactions"].([]any)
		if !ok || len(reactions) != 1 {
			t.Fatalf("%s: reactions = %v, want one", name, wireOf(t, m)["reactions"])
		}
		first := reactions[0].(map[string]any)
		if first["direction"] != "inbound" || first["emoji"] == "" || first["at"] != "2026-10-18T12:00:00Z" {
			t.Fatalf("%s: reaction = %v", name, first)
		}
	}
	if _, present := wireOf(t, &Message{ID: "m3", Text: "oi"})["reactions"]; present {
		t.Fatal("a message without reactions must not carry the field")
	}
}

func TestTheWireCarriesTheTranscriptAndTheEditStamp(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	m := &Message{ID: "m1", Text: AudioPlaceholder, Transcript: "quero o plano"}
	m.ApplyEdit("", at)
	wire := wireOf(t, m)
	if wire["transcript"] != "quero o plano" || wire["edited_at"] != "2026-10-18T12:00:00Z" {
		t.Fatalf("wire = %v", wire)
	}
	if _, present := wire["retracted_at"]; present {
		t.Fatal("a live message has no retracted_at")
	}
}

func TestARetractedMessageGoesOutWithoutItsContent(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	media := "media-1"
	m := &Message{
		ID: "m1", Channel: MessageChannelWhatsApp, Text: "📍 Casa, Rua A (-23.5, -46.6)", Transcript: "x",
		MediaID: &media, MediaType: MediaTypeImage, Media: &AttachedMedia{URL: "https://cdn/x.jpg"},
		Metadata: json.RawMessage(`{"extracted_text":"segredo"}`),
	}
	m.Retract(at)
	wire := wireOf(t, m)
	for _, hidden := range []string{"transcript", "mediaId", "mediaType", "media", "metadata", "location"} {
		if _, present := wire[hidden]; present {
			t.Errorf("%s must not reach the wire for a retracted message: %v", hidden, wire[hidden])
		}
	}
	if wire["text"] != "" {
		t.Errorf("text = %v, want empty", wire["text"])
	}
	if wire["retracted_at"] != "2026-10-18T12:00:00Z" || wire["id"] != "m1" {
		t.Errorf("the retraction itself must be visible: %v", wire)
	}
}

func TestTheWireNamesTheOperatorWhoSentTheMessage(t *testing.T) {
	cases := map[string]struct {
		sentBy SentBy
		want   any
	}{
		"an operator":  {SentByPerson("user-ana"), "user-ana"},
		"the customer": {SentByContact("5511999990000"), nil},
		"an AI agent":  {SentByAI("agent-1"), nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := wireOf(t, &Message{ID: "m1", Text: "oi", SentBy: tc.sentBy})["sender_user_id"]; got != tc.want {
				t.Fatalf("sender_user_id = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestARetractedMessageStillSaysWhoSentIt(t *testing.T) {
	m := &Message{ID: "m1", Text: "oi", SentBy: SentByPerson("user-ana")}
	m.Retract(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	if got := wireOf(t, m)["sender_user_id"]; got != "user-ana" {
		t.Fatalf("sender_user_id = %v, want user-ana", got)
	}
}

func TestAResentMessagePointsAtItsReplacement(t *testing.T) {
	m := &Message{ID: "m1", Text: "oi", SentBy: SentByPerson("user-ana"), DeliveryStatus: DeliveryStatusFailed}
	if err := m.MarkResentAs("m2"); err != nil {
		t.Fatal(err)
	}
	if m.ResentAs() != "m2" || wireOf(t, m)["resent_as"] != "m2" {
		t.Fatalf("resent_as = %q / %v, want m2", m.ResentAs(), wireOf(t, m)["resent_as"])
	}
	if _, present := wireOf(t, &Message{ID: "m3", Text: "oi"})["resent_as"]; present {
		t.Fatal("a message that was never resent must not carry resent_as")
	}
}

func TestTheWireCarriesTheClientRequestIDOfAnOperatorSend(t *testing.T) {
	if got := wireOf(t, &Message{ID: "m1", Text: "oi", ClientRequestID: "req-1"})["client_request_id"]; got != "req-1" {
		t.Fatalf("client_request_id = %v, want req-1", got)
	}
	if _, present := wireOf(t, &Message{ID: "m2", Text: "oi"})["client_request_id"]; present {
		t.Fatal("a message without a client request id must not carry the field")
	}
}

func TestStoredButtonsRoundTripThroughTheMetadata(t *testing.T) {
	m := &Message{ID: "m1", Text: "escolha"}
	input := SendButtonInput{BodyText: "escolha", FooterText: "rodapé", Buttons: []ButtonPayload{{ID: "b1", Title: "Sim"}}}
	if err := m.AttachButtons(input); err != nil {
		t.Fatal(err)
	}
	got, ok := m.Buttons()
	if !ok || got.BodyText != "escolha" || got.FooterText != "rodapé" || len(got.Buttons) != 1 || got.Buttons[0].Title != "Sim" {
		t.Fatalf("Buttons() = %+v, %v", got, ok)
	}
	if (&Message{ID: "m2", Text: "oi"}).HasButtons() {
		t.Fatal("a plain message has no buttons")
	}
}
