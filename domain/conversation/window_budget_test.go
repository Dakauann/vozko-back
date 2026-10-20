package conversation

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

func TestEvaluateWindowOutboundBudget(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)
	old := now.Add(-30 * time.Hour)
	timed := channel.Capabilities{OutboundWindow: 24 * time.Hour}
	untimed := channel.Capabilities{}

	cases := []struct {
		name       string
		caps       channel.Capabilities
		last       *time.Time
		sent       int
		wantOpen   bool
		wantReason WindowClosedReason
		wantExpiry bool
	}{
		{"unlimited ignores the count", timed, &recent, 50, true, WindowReasonNone, true},
		{"budget left", timed.WithOutboundBudget(2), &recent, 1, true, WindowReasonNone, true},
		{"budget spent", timed.WithOutboundBudget(2), &recent, 2, false, WindowReasonBudgetExhausted, false},
		{"budget overspent", timed.WithOutboundBudget(1), &recent, 3, false, WindowReasonBudgetExhausted, false},
		{"budget of one, nothing sent", untimed.WithOutboundBudget(1), &recent, 0, true, WindowReasonNone, false},
		{"budget of one spent on an untimed channel", untimed.WithOutboundBudget(1), &recent, 1, false, WindowReasonBudgetExhausted, false},
		{"untimed channel without inbound still counts", untimed.WithOutboundBudget(1), nil, 1, false, WindowReasonBudgetExhausted, false},
		{"expired wins over the budget", timed.WithOutboundBudget(1), &old, 1, false, WindowReasonExpired, false},
		{"no inbound wins over the budget", timed.WithOutboundBudget(1), nil, 1, false, WindowReasonNoInbound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateWindow(tc.caps, tc.last, tc.sent, now, false)
			if got.Open != tc.wantOpen || got.Reason != tc.wantReason {
				t.Fatalf("window = %+v, want open=%v reason=%q", got, tc.wantOpen, tc.wantReason)
			}
			if (got.ExpiresAt != nil) != tc.wantExpiry {
				t.Errorf("expiry = %v, want present=%v", got.ExpiresAt, tc.wantExpiry)
			}
			if !got.Open && got.Tier != channel.WindowTierNone {
				t.Errorf("a closed window has no tier, got %q", got.Tier)
			}
		})
	}
}

func TestClosedByProviderCarriesTheDetail(t *testing.T) {
	window := ClosedByProvider("blocked_by_buyer")
	if window.Open || window.Reason != WindowReasonProviderBlocked || window.ProviderDetail != "blocked_by_buyer" || window.ExpiresAt != nil {
		t.Fatalf("window = %+v", window)
	}
}

func TestNewReasonsAreClosedReasons(t *testing.T) {
	for _, reason := range []WindowClosedReason{WindowReasonProviderBlocked, WindowReasonBudgetExhausted} {
		window := ClosedWindow(reason)
		if window.Open || window.Reason != reason || window.ProviderDetail != "" {
			t.Errorf("ClosedWindow(%q) = %+v", reason, window)
		}
	}
	if WindowReasonProviderBlocked != "provider_blocked" || WindowReasonBudgetExhausted != "budget_exhausted" {
		t.Fatal("the wire values are fixed")
	}
}

type describedAdapter struct {
	fakeAdapter
	caps channel.Capabilities
}

func (d describedAdapter) Capabilities() channel.Capabilities { return d.caps }

func TestCapabilitiesOf(t *testing.T) {
	registry := NewAdapterRegistry(
		describedAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeTelegram}, caps: channel.Capabilities{MaxTextRunes: 4096}},
		fakeAdapter{entryType: shared.EntryTypeWebchat},
	)

	caps, err := CapabilitiesOf(registry, shared.EntryTypeTelegram)
	if err != nil || caps.MaxTextRunes != 4096 {
		t.Fatalf("caps = %+v err = %v", caps, err)
	}
	if _, err := CapabilitiesOf(registry, shared.EntryTypeWebchat); !errors.Is(err, ErrCapabilityUnsupported) {
		t.Errorf("an adapter that does not describe itself: err = %v, want ErrCapabilityUnsupported", err)
	}
	if _, err := CapabilitiesOf(registry, shared.EntryTypeInstagram); !errors.Is(err, ErrNoAdapterForEntryType) {
		t.Errorf("an unregistered entry type: err = %v, want ErrNoAdapterForEntryType", err)
	}
	if _, err := CapabilitiesOf(nil, shared.EntryTypeTelegram); !errors.Is(err, ErrNoAdapterForEntryType) {
		t.Errorf("no registry: err = %v, want ErrNoAdapterForEntryType", err)
	}
}
