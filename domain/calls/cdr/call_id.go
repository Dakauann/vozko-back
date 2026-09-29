package cdr

import (
	"strings"

	"github.com/google/uuid"
)

const (
	whatsAppOutboundCallIDPrefix = "wa-call-"
	whatsAppInboundCallIDPrefix  = "wa-in-"
	sipOutboundCallIDPrefix      = "sip-out-"
	sipInboundCallIDPrefix       = "sip-in-"
)

func IsWhatsAppCallID(callID string) bool {
	return strings.HasPrefix(callID, whatsAppOutboundCallIDPrefix) ||
		strings.HasPrefix(callID, whatsAppInboundCallIDPrefix)
}

func NewSIPOutboundCallID() string {
	return sipOutboundCallIDPrefix + uuid.NewString()
}

func SIPInboundCallID(dialogID string) string {
	return sipInboundCallIDPrefix + dialogID
}

func IsSIPCallID(callID string) bool {
	return strings.HasPrefix(callID, sipOutboundCallIDPrefix) ||
		strings.HasPrefix(callID, sipInboundCallIDPrefix)
}

func IsRecordedCallID(callID string) bool {
	return IsWhatsAppCallID(callID) || IsSIPCallID(callID)
}

func SourceForCallID(callID string) Source {
	switch {
	case IsWhatsAppCallID(callID):
		return SourceWhatsApp
	case IsSIPCallID(callID):
		return SourceSIPTrunk
	}
	return SourceWebSocket
}
