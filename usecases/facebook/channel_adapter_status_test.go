package facebook

import (
	"context"
	"testing"
	"time"

	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
)

func TestWindowStateFoldsInThePageStatus(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*fbdomain.Page)
		wantOpen bool
	}{
		{"connected with messaging", func(*fbdomain.Page) {}, true},
		{"pending", func(p *fbdomain.Page) { p.Status = fbdomain.StatusPending }, false},
		{"token revoked", func(p *fbdomain.Page) { p.Status = fbdomain.StatusTokenRevoked }, false},
		{"needs role", func(p *fbdomain.Page) { p.Status = fbdomain.StatusNeedsRole }, false},
		{"restricted", func(p *fbdomain.Page) { p.Status = fbdomain.StatusRestricted }, false},
		{"disconnected", func(p *fbdomain.Page) { p.Status = fbdomain.StatusDisconnected }, false},
		{"messaging scope missing", func(p *fbdomain.Page) { p.GrantedScopes = []string{fbdomain.ScopeManageMetadata} }, false},
		{"no page token", func(p *fbdomain.Page) { p.PageToken = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdapterFixture(t, false, time.Hour)
			page := f.pages.byID["page-1"]
			tc.mutate(page)

			window, err := f.adapter.WindowState(context.Background(), f.ec)
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

func TestWindowStateClosesWhenThePageCannotBeRead(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	ec := *f.ec
	ec.AccountID = "missing"

	window, err := f.adapter.WindowState(context.Background(), &ec)
	if err == nil || window.Open || window.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("window = %+v err = %v, want closed channel_unavailable with the error", window, err)
	}
}

func TestAdapterDescribesTheMessengerCapabilities(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	caps, err := conversation.CapabilitiesOf(conversation.NewAdapterRegistry(f.adapter), shared.EntryTypeFacebook)
	if err != nil {
		t.Fatalf("CapabilitiesOf: %v", err)
	}
	if caps.MaxTextRunes != fbdomain.MaxTextRunes {
		t.Errorf("caps = %+v, want the Messenger text limit", caps)
	}
}
