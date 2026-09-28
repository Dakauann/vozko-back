package facebook

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

func TestDescriptorIdentifiesMessenger(t *testing.T) {
	d := Descriptor(false)
	if d.Kind != channel.KindFacebook || d.EntryType != shared.EntryTypeFacebook {
		t.Fatalf("got %s / %s", d.Kind, d.EntryType)
	}
	if d.Capabilities.CanInitiateConversation || d.Capabilities.SupportsTemplates {
		t.Fatal("messenger cannot open a conversation or send templates")
	}
	if !strings.Contains(d.InboxSQL.EntryJoin, "facebook_pages fbp") {
		t.Fatalf("inbox join = %q", d.InboxSQL.EntryJoin)
	}
}

func TestHumanAgentTierFollowsTheApprovalSwitch(t *testing.T) {
	last := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	threeDaysLater := last.Add(72 * time.Hour)

	if tier, _ := Descriptor(false).Capabilities.Window(&last, threeDaysLater, true); tier != channel.WindowTierNone {
		t.Fatalf("unapproved human agent opened tier %q", tier)
	}
	if tier, _ := Descriptor(true).Capabilities.Window(&last, threeDaysLater, true); tier != channel.WindowTierHuman {
		t.Fatalf("approved operator got tier %q", tier)
	}
	if tier, _ := Descriptor(true).Capabilities.Window(&last, threeDaysLater, false); tier != channel.WindowTierNone {
		t.Fatalf("automation got tier %q after 24h", tier)
	}
}

func TestSendTierForWindowTier(t *testing.T) {
	cases := map[channel.WindowTier]SendTier{
		channel.WindowTierStandard: SendStandard,
		channel.WindowTierHuman:    SendHumanAgent,
	}
	for window, want := range cases {
		got, ok := SendTierFor(window)
		if !ok || got != want {
			t.Errorf("%q gave %q, %v", window, got, ok)
		}
	}
	if _, ok := SendTierFor(channel.WindowTierNone); ok {
		t.Error("a closed window must not map to a send tier")
	}
}

func TestInteractiveStylesMapToTheirMessengerShapes(t *testing.T) {
	limits := Descriptor(false).Capabilities.Interactive
	if limits.MaxOptionsFor(channel.InteractiveStyleButtons) != MaxTemplateButtons {
		t.Errorf("buttons allow %d", limits.MaxOptionsFor(channel.InteractiveStyleButtons))
	}
	if limits.MaxOptionsFor(channel.InteractiveStyleList) != MaxQuickReplies {
		t.Errorf("list allows %d", limits.MaxOptionsFor(channel.InteractiveStyleList))
	}
}

func TestOutboundMetadataNamesWhoSent(t *testing.T) {
	if OutboundMetadata(true) != "vozko:operator" || OutboundMetadata(false) != "vozko:automation" {
		t.Fatalf("got %q / %q", OutboundMetadata(true), OutboundMetadata(false))
	}
}

func TestFailuresThatChangeThePageStatus(t *testing.T) {
	cases := map[Failure]Status{
		FailureReauth:         StatusTokenRevoked,
		FailureRoleLost:       StatusNeedsRole,
		FailurePageRestricted: StatusRestricted,
	}
	for failure, want := range cases {
		if got, ok := PageStatusAfter(failure); !ok || got != want {
			t.Errorf("%q gave %q, %v", failure, got, ok)
		}
	}
	for _, failure := range []Failure{FailureRetryable, FailureWindowClosed, FailureUnreachable, FailureThreadControl, FailureUnknown} {
		if _, ok := PageStatusAfter(failure); ok {
			t.Errorf("%q must not change the page status", failure)
		}
	}
}
