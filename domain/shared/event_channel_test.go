package shared

import "testing"

// The channel label on a timeline event was derived twice, both times as a
// switch listing a subset of the entry types with `default: "whatsapp"`. An
// operator's Telegram reply was therefore recorded as a WhatsApp event, and
// every report grouped by channel counted it under WhatsApp.
//
// The label is persisted on the event, so a wrong one is not a display glitch,
// it is wrong history.

func TestEveryKnownEntryTypeNamesItsOwnChannel(t *testing.T) {
	for _, et := range KnownEntryTypes() {
		if got := et.EventChannel(); got != string(et) {
			t.Errorf("%s.EventChannel() = %q, want its own name", et, got)
		}
	}
}

// The regression by name: these are the two that inherited the default.
func TestTelegramAndInstagramAreNotLabelledWhatsApp(t *testing.T) {
	for _, et := range []EntryType{EntryTypeTelegram, EntryTypeInstagram} {
		if got := et.EventChannel(); got == string(EntryTypeWhatsApp) {
			t.Errorf("%s.EventChannel() = %q, this is the shipped bug", et, got)
		}
	}
}

// A channel added later must name itself without anyone editing a switch.
// KnownEntryTypes is the single list, so this holds by construction; the test
// exists so that if the identity is ever replaced by a lookup, a missing entry
// fails here rather than in stored history.
func TestAChannelAddedLaterDoesNotSilentlyBecomeWhatsApp(t *testing.T) {
	if len(KnownEntryTypes()) < 5 {
		t.Fatalf("expected the full entry type set, got %v", KnownEntryTypes())
	}
	for _, et := range KnownEntryTypes() {
		if et == EntryTypeWhatsApp {
			continue
		}
		if et.EventChannel() == string(EntryTypeWhatsApp) {
			t.Errorf("%s falls back to whatsapp", et)
		}
	}
}

// Unknown values keep the historical fallback rather than writing an empty
// label into a stored event.
func TestAnUnknownEntryTypeKeepsTheHistoricalFallback(t *testing.T) {
	if got := EntryType("carrier-pigeon").EventChannel(); got != string(EntryTypeWhatsApp) {
		t.Errorf("EventChannel() = %q, want the whatsapp fallback", got)
	}
	if got := EntryType("").EventChannel(); got != string(EntryTypeWhatsApp) {
		t.Errorf("empty EventChannel() = %q, want the whatsapp fallback", got)
	}
}
