package callsession

import cdr "vozko/domain/calls/cdr"

const (
	OfferChannelSIP      = "sip"
	OfferChannelWhatsApp = "whatsapp"
)

func OfferChannels() []string {
	return []string{OfferChannelSIP, OfferChannelWhatsApp}
}

func OfferChannelFor(callID string) string {
	switch cdr.SourceForCallID(callID) {
	case cdr.SourceSIPTrunk:
		return OfferChannelSIP
	case cdr.SourceWhatsApp:
		return OfferChannelWhatsApp
	}
	return ""
}
