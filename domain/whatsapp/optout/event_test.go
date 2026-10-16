package optout

import (
	"errors"
	"testing"
	"time"
)

func TestParsePreferenceKnowsOnlyStopAndResume(t *testing.T) {
	for raw, want := range map[string]Preference{"stop": PreferenceStop, " RESUME ": PreferenceResume} {
		if got, ok := ParsePreference(raw); !ok || got != want {
			t.Fatalf("%q: got %q, %v", raw, got, ok)
		}
	}
	if _, ok := ParsePreference("pause"); ok {
		t.Fatal("pause is not a WhatsApp preference")
	}
}

func TestHashRecipientNeverKeepsTheNumber(t *testing.T) {
	hash := HashRecipient("5511999990001")
	if len(hash) != 64 || hash == "5511999990001" || hash != HashRecipient(" 5511999990001 ") {
		t.Fatalf("hash %q", hash)
	}
	if HashRecipient("5511999990001") == HashRecipient("5511999990002") {
		t.Fatal("different recipients must not share a hash")
	}
}

func TestEventValidateRefusesWhatCannotBeAttributed(t *testing.T) {
	valid := Event{PhoneNumberID: "1179270238614202", RecipientHash: HashRecipient("5511"), Category: "marketing_messages", Preference: PreferenceStop, OccurredAt: time.Unix(1, 0)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, breakIt := range map[string]func(*Event){
		"no phone":     func(e *Event) { e.PhoneNumberID = "" },
		"no recipient": func(e *Event) { e.RecipientHash = "" },
		"unknown":      func(e *Event) { e.Preference = "pause" },
		"no moment":    func(e *Event) { e.OccurredAt = time.Time{} },
	} {
		e := valid
		breakIt(&e)
		if !errors.Is(e.Validate(), ErrInvalidEvent) {
			t.Fatalf("%s: want ErrInvalidEvent", name)
		}
	}
}
