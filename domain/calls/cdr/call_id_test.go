package cdr

import "testing"

func TestIsWhatsAppCallID(t *testing.T) {
	whatsapp := []string{
		"wa-call-1781206757917487400",
		"wa-in-wacid.ABGGFjFVU2AfAgo6V-Hc5eCgK5Gh",
	}
	notWhatsApp := []string{
		"",
		"sip-12345",
		"call_abc",
		"wacid.ABGGFjFVU2AfAgo6V",
		"WA-CALL-123",
		"prefix-wa-call-123",
		// Historical "sip-in-" CDR ids must NOT be treated as WhatsApp, so they
		// keep classifying at the non-WhatsApp telephony rate.
		"sip-in-9f2c1e4a-1234-4abc-8def-0123456789ab",
	}
	for _, id := range whatsapp {
		if !IsWhatsAppCallID(id) {
			t.Errorf("IsWhatsAppCallID(%q) = false, want true", id)
		}
	}
	for _, id := range notWhatsApp {
		if IsWhatsAppCallID(id) {
			t.Errorf("IsWhatsAppCallID(%q) = true, want false", id)
		}
	}
}

func TestSourceForCallID(t *testing.T) {
	if got := SourceForCallID("wa-call-123"); got != SourceWhatsApp {
		t.Errorf("outbound WhatsApp source = %q, want %q", got, SourceWhatsApp)
	}
	if got := SourceForCallID("wa-in-abc"); got != SourceWhatsApp {
		t.Errorf("inbound WhatsApp source = %q, want %q", got, SourceWhatsApp)
	}
	if got := SourceForCallID("sip-123"); got != SourceWebSocket {
		t.Errorf("SIP source = %q, want %q", got, SourceWebSocket)
	}
}
