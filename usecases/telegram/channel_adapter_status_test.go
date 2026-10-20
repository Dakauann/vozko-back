package telegram

import (
	"context"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
)

func TestWindowStateFoldsInTheAccountStatus(t *testing.T) {
	cases := []struct {
		name     string
		account  func() *tgdomain.Account
		wantOpen bool
	}{
		{"active bot", botAccount, true},
		{"webhook failing still sends", func() *tgdomain.Account {
			a := botAccount()
			a.Status = tgdomain.StatusWebhookFailing
			return a
		}, true},
		{"pending bot", func() *tgdomain.Account {
			a := botAccount()
			a.Status = tgdomain.StatusPending
			return a
		}, false},
		{"token invalid", func() *tgdomain.Account {
			a := botAccount()
			a.Status = tgdomain.StatusTokenInvalid
			return a
		}, false},
		{"revoked", func() *tgdomain.Account {
			a := botAccount()
			a.Status = tgdomain.StatusRevoked
			return a
		}, false},
		{"no bot token", func() *tgdomain.Account {
			a := botAccount()
			a.BotToken = ""
			return a
		}, false},
		{"business account with token invalid", func() *tgdomain.Account {
			a := businessAccount(true)
			a.Status = tgdomain.StatusTokenInvalid
			return a
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conv := conversationFor(5041234567)
			contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567}
			adapter, _, _, _, _, _ := newAdapter(tc.account(), conv, contact)
			fresh := time.Now().UTC().Add(-time.Minute)

			window, err := adapter.WindowState(context.Background(), entryContext(conv, &fresh))
			if err != nil {
				t.Fatalf("WindowState: %v", err)
			}
			if window.Open != tc.wantOpen {
				t.Fatalf("window = %+v, want open=%v", window, tc.wantOpen)
			}
			if !tc.wantOpen && (window.Reason != conversation.WindowReasonChannelUnavailable || window.ExpiresAt != nil) {
				t.Errorf("window = %+v, want channel_unavailable with no time", window)
			}
		})
	}
}

func TestAdapterDescribesTheTelegramCapabilities(t *testing.T) {
	conv := conversationFor(5041234567)
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, &tgdomain.Contact{ID: "contact-1"})
	caps, err := conversation.CapabilitiesOf(conversation.NewAdapterRegistry(adapter), shared.EntryTypeTelegram)
	if err != nil {
		t.Fatalf("CapabilitiesOf: %v", err)
	}
	if caps.MaxTextRunes != tgdomain.Descriptor().Capabilities.MaxTextRunes || caps.MaxTextRunes == 0 {
		t.Errorf("caps = %+v, want the Telegram text limit", caps)
	}
}
