package callsession

import "testing"

func TestEveryCallIsOfferedUnderOneChannelName(t *testing.T) {
	cases := map[string]string{
		"sip-in-abc":   OfferChannelSIP,
		"sip-out-abc":  OfferChannelSIP,
		"wa-in-abc":    OfferChannelWhatsApp,
		"wa-call-abc":  OfferChannelWhatsApp,
		"browser-call": "",
	}
	for callID, want := range cases {
		if got := OfferChannelFor(callID); got != want {
			t.Errorf("%s offered as %q, want %q", callID, got, want)
		}
	}
}
