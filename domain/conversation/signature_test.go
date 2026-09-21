package conversation

import (
	"testing"

	"vozko/domain/shared"
)

func TestSignOutboundPerChannelFormat(t *testing.T) {
	cases := []struct {
		name      string
		entryType shared.EntryType
		want      string
	}{
		{"instagram has no bold markup", shared.EntryTypeInstagram, "Ana:\noi"},
		{"whatsapp renders bold", shared.EntryTypeWhatsApp, "*Ana*:\noi"},
		{"telegram falls back to the bold form", shared.EntryTypeTelegram, "*Ana*:\noi"},
		{"unofficial whatsapp is still whatsapp", shared.EntryTypeUnofficialWhatsApp, "*Ana*:\noi"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SignOutbound(tc.entryType, "Ana", "oi"); got != tc.want {
				t.Errorf("SignOutbound(%q) = %q, want %q", tc.entryType, got, tc.want)
			}
		})
	}
}

func TestSignOutboundWithoutUsernameLeavesTextAlone(t *testing.T) {
	for _, username := range []string{"", "   "} {
		if got := SignOutbound(shared.EntryTypeWhatsApp, username, "oi"); got != "oi" {
			t.Errorf("SignOutbound(username=%q) = %q, want the text unchanged", username, got)
		}
	}
}
