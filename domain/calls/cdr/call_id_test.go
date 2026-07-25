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
		// Receptive SIP inbound calls must NOT be treated as WhatsApp, so they
		// bill at the sip_trunk telephony rate.
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

func TestInboundSIPCallID(t *testing.T) {
	id := NewInboundSIPCallID("9f2c1e4a")
	if id != "sip-in-9f2c1e4a" {
		t.Fatalf("NewInboundSIPCallID = %q, want %q", id, "sip-in-9f2c1e4a")
	}
	if !IsInboundSIPCallID(id) {
		t.Errorf("IsInboundSIPCallID(%q) = false, want true", id)
	}
	if IsWhatsAppCallID(id) {
		t.Errorf("IsWhatsAppCallID(%q) = true, want false (must bill as sip_trunk)", id)
	}
	if IsInboundSIPCallID("wa-in-abc") {
		t.Errorf("IsInboundSIPCallID(wa-in-abc) = true, want false")
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
