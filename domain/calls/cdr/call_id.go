package cdr

import "strings"

const (
	whatsAppOutboundCallIDPrefix = "wa-call-"
	whatsAppInboundCallIDPrefix  = "wa-in-"
	// inboundSIPCallIDPrefix marks a receptive (inbound) SIP-trunk call. It is
	// deliberately NOT a WhatsApp prefix, so IsWhatsAppCallID stays false and the
	// call bills at the sip_trunk telephony rate (never whatsapp_calls).
	inboundSIPCallIDPrefix = "sip-in-"
)

func IsWhatsAppCallID(callID string) bool {
	return strings.HasPrefix(callID, whatsAppOutboundCallIDPrefix) ||
		strings.HasPrefix(callID, whatsAppInboundCallIDPrefix)
}

// NewInboundSIPCallID builds a unique call ID for a receptive (inbound) SIP call
// from a unique suffix (e.g. a UUID). The "sip-in-" prefix keeps the call out of
// the WhatsApp pricing path while remaining unique, so the billing consumer's
// per-call-id idempotency holds.
func NewInboundSIPCallID(unique string) string {
	return inboundSIPCallIDPrefix + unique
}

// IsInboundSIPCallID reports whether a call ID was minted by NewInboundSIPCallID.
func IsInboundSIPCallID(callID string) bool {
	return strings.HasPrefix(callID, inboundSIPCallIDPrefix)
}

func SourceForCallID(callID string) Source {
	if IsWhatsAppCallID(callID) {
		return SourceWhatsApp
	}
	return SourceWebSocket
}
