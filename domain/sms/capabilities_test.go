package sms

import (
	"strings"
	"testing"
)

func TestSMSCapabilities(t *testing.T) {
	caps := Capabilities()
	if !caps.CanInitiateConversation || caps.OutboundWindow != 0 || len(caps.MediaLimits) != 0 || caps.Interactive.PresentsChoices() {
		t.Fatalf("caps = %+v", caps)
	}
	if caps.TextTooLong(strings.Repeat("a", 153*MaxSegments)) {
		t.Fatal("eight GSM parts fit")
	}
	violations := caps.TextViolations(strings.Repeat("ã", 67*MaxSegments+1))
	if len(violations) == 0 || violations[0].Code != TextViolationSegments {
		t.Fatalf("violations = %+v", violations)
	}
}
