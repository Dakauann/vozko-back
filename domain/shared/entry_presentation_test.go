package shared

import "testing"

func TestSignatureStyleIsPlainOnlyWhereMarkupIsNotRendered(t *testing.T) {
	cases := map[EntryType]bool{
		EntryTypeInstagram:          true,
		EntryTypeWhatsApp:           false,
		EntryTypeTelegram:           false,
		EntryTypeUnofficialWhatsApp: false,
	}
	for e, want := range cases {
		if got := e.SignsWithPlainText(); got != want {
			t.Errorf("%s plain signature = %t, want %t", e, got, want)
		}
	}
}

func TestDisplayLabelFallsBackToTheRawType(t *testing.T) {
	cases := map[EntryType]string{
		EntryTypeInstagram: "Instagram",
		EntryTypeTelegram:  "Telegram",
		"future_channel":   "future_channel",
	}
	for e, want := range cases {
		if got := e.DisplayLabel(); got != want {
			t.Errorf("%s label = %q, want %q", e, got, want)
		}
	}
}
