package tiktok

import (
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
)

func TestChannelCapabilitiesEncodeThePlatformLimits(t *testing.T) {
	caps := ChannelCapabilities()
	if caps.CanInitiateConversation {
		t.Error("tiktok forbids business-initiated conversations")
	}
	if caps.OutboundWindow != 48*time.Hour || caps.OutboundBudget != 10 || caps.HumanAgentWindow {
		t.Errorf("window = %v budget = %d human = %v", caps.OutboundWindow, caps.OutboundBudget, caps.HumanAgentWindow)
	}
	if caps.MaxTextRunes != 6000 {
		t.Errorf("max text runes = %d", caps.MaxTextRunes)
	}
	image, ok := caps.MediaLimits[channel.MediaImage]
	if !ok || image.MaxBytes != 3*1024*1024 || !image.Allows("image/png") || !image.Allows("image/jpeg") || image.Allows("image/webp") {
		t.Errorf("image limit = %+v", image)
	}
	for _, kind := range []channel.MediaKind{channel.MediaVideo, channel.MediaAudio, channel.MediaDocument} {
		if _, ok := caps.MediaLimits[kind]; ok {
			t.Errorf("tiktok must not advertise outbound %s", kind)
		}
	}
	if caps.Interactive.MaxOptionsButtons != 3 || caps.Interactive.MaxOptionsList != 0 || caps.Interactive.MaxLabelRunes != 20 {
		t.Errorf("interactive = %+v", caps.Interactive)
	}
}

func TestConversationWindow(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	recent := ptrTime(now.Add(-time.Hour))
	old := ptrTime(now.Add(-49 * time.Hour))
	cases := []struct {
		name string
		conv Conversation
		open bool
		why  conversation.WindowClosedReason
	}{
		{"open after a customer message", Conversation{ConversationID: "c1", LastCustomerMessageAt: recent}, true, ""},
		{"never written", Conversation{ConversationID: "c1"}, false, conversation.WindowReasonNoInbound},
		{"expired after 48h", Conversation{ConversationID: "c1", LastCustomerMessageAt: old}, false, conversation.WindowReasonExpired},
		{"budget spent", Conversation{ConversationID: "c1", LastCustomerMessageAt: recent, OutboundSinceInbound: OutboundBudget}, false, conversation.WindowReasonBudgetExhausted},
		{"nine sent", Conversation{ConversationID: "c1", LastCustomerMessageAt: recent, OutboundSinceInbound: OutboundBudget - 1}, true, ""},
		{"closed by tiktok after the last customer message", Conversation{ConversationID: "c1", LastCustomerMessageAt: recent, BudgetClosedAt: ptrTime(now.Add(-time.Minute))}, false, conversation.WindowReasonBudgetExhausted},
		{"customer wrote after tiktok closed it", Conversation{ConversationID: "c1", LastCustomerMessageAt: ptrTime(now.Add(-time.Minute)), BudgetClosedAt: ptrTime(now.Add(-time.Hour))}, true, ""},
		{"no tiktok conversation id", Conversation{LastCustomerMessageAt: recent}, false, conversation.WindowReasonChannelUnavailable},
	}
	for _, tc := range cases {
		got := tc.conv.Window(now)
		if got.Open != tc.open || got.Reason != tc.why {
			t.Errorf("%s: Window = %+v, want open=%v reason=%q", tc.name, got, tc.open, tc.why)
		}
	}
}

func TestOpenWindowExpiresFortyEightHoursAfterTheCustomer(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Hour)
	got := (&Conversation{ConversationID: "c1", LastCustomerMessageAt: &last}).Window(now)
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(last.Add(48*time.Hour)) {
		t.Fatalf("ExpiresAt = %v", got.ExpiresAt)
	}
}

func TestImageCapabilityIsTrustedOnlyWhileFresh(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	yes, no := true, false
	cases := []struct {
		name      string
		allowed   *bool
		checked   *time.Time
		known     bool
		allowedOK bool
	}{
		{"never checked", nil, nil, false, false},
		{"fresh yes", &yes, ptrTime(now.Add(-time.Hour)), true, true},
		{"fresh no", &no, ptrTime(now.Add(-time.Hour)), true, false},
		{"stale yes", &yes, ptrTime(now.Add(-ImageCapabilityFreshness - time.Minute)), false, false},
		{"answer without a time", &yes, nil, false, false},
	}
	for _, tc := range cases {
		c := Conversation{ImageSendAllowed: tc.allowed, ImageSendCheckedAt: tc.checked}
		allowed, known := c.ImageSendKnown(now)
		if allowed != tc.allowedOK || known != tc.known {
			t.Errorf("%s: ImageSendKnown = %v, %v", tc.name, allowed, known)
		}
	}
}

func TestConversationTypeParsing(t *testing.T) {
	for raw, want := range map[string]ConversationType{"STRANGER": ConversationStranger, "SINGLE": ConversationSingle, "single": ConversationSingle, "": ConversationUnknown, "GROUP": ConversationUnknown} {
		if got := ParseConversationType(raw); got != want {
			t.Errorf("ParseConversationType(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestContactDisplayName(t *testing.T) {
	if got := (&Contact{DisplayName: " Ana ", Username: "ana"}).Name(); got != "Ana" {
		t.Errorf("Name = %q", got)
	}
	if got := (&Contact{Username: "ana"}).Name(); got != "@ana" {
		t.Errorf("Name = %q", got)
	}
	if got := (&Contact{}).Name(); got != "" {
		t.Errorf("Name = %q", got)
	}
}

func TestContactProfileFreshness(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if !(&Contact{}).ProfileIsStale(now) {
		t.Error("a contact never refreshed is stale")
	}
	if (&Contact{ProfileRefreshedAt: ptrTime(now.Add(-time.Hour))}).ProfileIsStale(now) {
		t.Error("a contact refreshed an hour ago is fresh")
	}
	if !(&Contact{ProfileRefreshedAt: ptrTime(now.Add(-ProfileFreshness))}).ProfileIsStale(now) {
		t.Error("a contact refreshed exactly one freshness period ago is stale")
	}
}
