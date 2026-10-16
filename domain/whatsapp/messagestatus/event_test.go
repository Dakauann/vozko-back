package messagestatus

import (
	"errors"
	"testing"
	"time"
)

func TestParseStatusAcceptsOnlyMetaDeliveryStatuses(t *testing.T) {
	for raw, want := range map[string]Status{"sent": StatusSent, " DELIVERED ": StatusDelivered, "Read": StatusRead, "failed": StatusFailed} {
		if got, ok := ParseStatus(raw); !ok || got != want {
			t.Fatalf("%q: got %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "deleted", "warning", "played"} {
		if _, ok := ParseStatus(raw); ok {
			t.Fatalf("%q is not a delivery status", raw)
		}
	}
}

func TestParseOccurredAtReadsMetaUnixSeconds(t *testing.T) {
	got, ok := ParseOccurredAt("1791990878")
	if !ok || !got.Equal(time.Unix(1791990878, 0)) {
		t.Fatalf("got %v, %v", got, ok)
	}
	for _, raw := range []string{"", "abc", "0", "-5"} {
		if _, ok := ParseOccurredAt(raw); ok {
			t.Fatalf("%q is not a moment", raw)
		}
	}
}

func TestEventValidateRefusesWhatCannotBeAttributed(t *testing.T) {
	valid := Event{MessageID: "wamid.1", PhoneNumberID: "1179270238614202", Status: StatusRead, OccurredAt: time.Unix(1, 0)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid event: %v", err)
	}
	cases := map[string]func(*Event){
		"no message":     func(e *Event) { e.MessageID = " " },
		"no phone":       func(e *Event) { e.PhoneNumberID = "" },
		"unknown status": func(e *Event) { e.Status = "played" },
		"no moment":      func(e *Event) { e.OccurredAt = time.Time{} },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := valid
			breakIt(&e)
			if !errors.Is(e.Validate(), ErrInvalidEvent) {
				t.Fatalf("want ErrInvalidEvent, got %v", e.Validate())
			}
		})
	}
}
