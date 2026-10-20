package instagram

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

func TestWindowStateFoldsInTheAccountStatus(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*igdomain.Account)
		wantOpen bool
	}{
		{"connected with messaging", func(*igdomain.Account) {}, true},
		{"pending", func(a *igdomain.Account) { a.Status = igdomain.StatusPending }, false},
		{"token expired", func(a *igdomain.Account) { a.Status = igdomain.StatusTokenExpired }, false},
		{"revoked", func(a *igdomain.Account) { a.Status = igdomain.StatusRevoked }, false},
		{"suspended", func(a *igdomain.Account) { a.Status = igdomain.StatusSuspended }, false},
		{"messaging scope missing", func(a *igdomain.Account) { a.GrantedScopes = []string{igdomain.ScopeBasic} }, false},
		{"no access token", func(a *igdomain.Account) { a.AccessToken = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := connectedAccount()
			tc.mutate(account)
			f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
			ec, err := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
			if err != nil {
				t.Fatalf("ResolveEntry: %v", err)
			}

			window, err := f.adapter.WindowState(context.Background(), ec)
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

func TestWindowStateClosesWhenTheAccountCannotBeRead(t *testing.T) {
	f := newAdapterFixture(t, nil, openWindow())
	ec := &conversation.EntryContext{EntryID: "conv-x", EntryType: shared.EntryTypeInstagram, AccountID: "missing", LastInboundAt: openWindow()}

	window, err := f.adapter.WindowState(context.Background(), ec)
	if err == nil || window.Open || window.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("window = %+v err = %v, want closed channel_unavailable with the error", window, err)
	}
}

func TestAdapterDescribesTheInstagramCapabilities(t *testing.T) {
	f := newAdapterFixture(t, []*igdomain.Account{connectedAccount()}, openWindow())
	caps, err := conversation.CapabilitiesOf(conversation.NewAdapterRegistry(f.adapter), shared.EntryTypeInstagram)
	if err != nil {
		t.Fatalf("CapabilitiesOf: %v", err)
	}
	if caps.MaxTextBytes != igdomain.Descriptor().Capabilities.MaxTextBytes || caps.MaxTextBytes == 0 {
		t.Errorf("caps = %+v, want the descriptor's text limit", caps)
	}
}
