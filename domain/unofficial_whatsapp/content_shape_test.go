package unofficial_whatsapp

import "testing"

// The vendor sends `content` as EITHER a string or an object, and the object
// form is what a reply, a quote, a link preview or any media message produces.
//
// Declared as a plain string, the object form failed the whole struct, and
// decodeMessages turns any unmarshal failure into "no messages at all": the
// message was discarded with no error and no log. Group chatter survived because
// it is mostly plain text, while a direct thread where people quote and link
// each other stopped reaching the CRM entirely.
func TestMessageSurvivesBothContentShapes(t *testing.T) {
	const chat = `"chatid":"5511999999999@s.whatsapp.net","messageid":"ABC","isGroup":false,"fromMe":false,"sender":"1234@lid","sender_pn":"5511999999999@s.whatsapp.net","messageTimestamp":1786137401000`

	cases := map[string]struct {
		body     string
		wantText string
	}{
		"content as string": {
			body:     `{"EventType":"messages","message":{` + chat + `,"messageType":"Conversation","text":"bom dia","content":"bom dia"}}`,
			wantText: "bom dia",
		},
		"content as object": {
			body:     `{"EventType":"messages","message":{` + chat + `,"messageType":"ExtendedTextMessage","text":"bom dia","content":{"text":"bom dia","contextInfo":{"forwardingScore":2}}}}`,
			wantText: "bom dia",
		},
		"content as object, text only inside it": {
			body:     `{"EventType":"messages","message":{` + chat + `,"messageType":"ExtendedTextMessage","text":"","buttonOrListid":"opt-1","content":{"text":"Agendar"}}}`,
			wantText: "Agendar",
		},
		// A shape nobody has seen must cost that one field, never the message.
		"content as an unexpected type": {
			body:     `{"EventType":"messages","message":{` + chat + `,"messageType":"Conversation","text":"bom dia","content":42}}`,
			wantText: "bom dia",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, err := DecodeEnvelope([]byte(tc.body))
			if err != nil {
				t.Fatalf("DecodeEnvelope: %v", err)
			}
			evs := NormalizeEnvelope("inst-1", env)
			if len(evs) != 1 {
				t.Fatalf("got %d events, want 1 — the message was discarded", len(evs))
			}
			if evs[0].Text != tc.wantText {
				t.Errorf("Text = %q, want %q", evs[0].Text, tc.wantText)
			}
			if evs[0].Kind != EventInboundMessage {
				t.Errorf("Kind = %v, want %v", evs[0].Kind, EventInboundMessage)
			}
		})
	}
}
