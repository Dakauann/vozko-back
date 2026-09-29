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

func TestSIPCallIDsHaveTheirOwnSourceAndAreRecorded(t *testing.T) {
	for _, id := range []string{NewSIPOutboundCallID(), SIPInboundCallID("dialog-1")} {
		if !IsSIPCallID(id) || IsWhatsAppCallID(id) {
			t.Errorf("%q: IsSIPCallID=%v IsWhatsAppCallID=%v, want a SIP id only", id, IsSIPCallID(id), IsWhatsAppCallID(id))
		}
		if SourceForCallID(id) != SourceSIPTrunk {
			t.Errorf("SourceForCallID(%q) = %q, want sip_trunk", id, SourceForCallID(id))
		}
		if !IsRecordedCallID(id) {
			t.Errorf("IsRecordedCallID(%q) = false, want SIP calls recorded", id)
		}
	}
	if NewSIPOutboundCallID() == NewSIPOutboundCallID() {
		t.Error("outbound SIP call ids must be unique")
	}
	if !IsRecordedCallID("wa-call-1") || IsRecordedCallID("call_abc") || IsSIPCallID("sip-12345") {
		t.Error("recording must cover WhatsApp and SIP call ids only")
	}
}
