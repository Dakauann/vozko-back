package cdr

import "strings"

const (
	whatsAppOutboundCallIDPrefix = "wa-call-"
	whatsAppInboundCallIDPrefix  = "wa-in-"
)

func IsWhatsAppCallID(callID string) bool {
	return strings.HasPrefix(callID, whatsAppOutboundCallIDPrefix) ||
		strings.HasPrefix(callID, whatsAppInboundCallIDPrefix)
}

func SourceForCallID(callID string) Source {
	if IsWhatsAppCallID(callID) {
		return SourceWhatsApp
	}
	return SourceWebSocket
}
