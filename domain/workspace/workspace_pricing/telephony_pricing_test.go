package workspace_pricing

import "testing"

func TestTelephonyChannelsPriceFromTheirOwnLine(t *testing.T) {
	cases := map[string]string{
		TelephonyChannelWhatsApp: TelephonyServiceWhatsAppCalls,
		TelephonyChannelSIP:      TelephonyServiceSIPCalls,
	}
	for channel, want := range cases {
		if got := TelephonyServiceForChannel(channel); got != want {
			t.Errorf("TelephonyServiceForChannel(%q) = %q, want %q", channel, got, want)
		}
	}
	for _, unknown := range []string{"", "skype"} {
		if got := TelephonyServiceForChannel(unknown); got != "" {
			t.Errorf("TelephonyServiceForChannel(%q) = %q, want no service so pricing fails closed", unknown, got)
		}
	}
}

func TestSIPMinutesAreACatalogLineSeededUnpriced(t *testing.T) {
	item := catalogEntry(t, CategoryTelephony, TelephonyServiceSIPCalls, "per_minute")
	if item.Currency != "USD" {
		t.Errorf("currency = %q, want USD like WhatsApp minutes", item.Currency)
	}
	if item.PriceMicros != 0 || item.CostMicros != 0 {
		t.Errorf("sip_calls seeded at cost %d price %d, want 0/0 until a plan prices it", item.CostMicros, item.PriceMicros)
	}
}
