package shared

import "testing"

func TestEveryKnownEntryTypeNamesItsOwnChannel(t *testing.T) {
	for _, et := range KnownEntryTypes() {
		if got := et.EventChannel(); got != string(et) {
			t.Errorf("%s.EventChannel() = %q, want its own name", et, got)
		}
	}
}

func TestTelegramAndInstagramAreNotLabelledWhatsApp(t *testing.T) {
	for _, et := range []EntryType{EntryTypeTelegram, EntryTypeInstagram} {
		if got := et.EventChannel(); got == string(EntryTypeWhatsApp) {
			t.Errorf("%s.EventChannel() = %q, this is the shipped bug", et, got)
		}
	}
}

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

func TestAnUnknownEntryTypeKeepsTheHistoricalFallback(t *testing.T) {
	if got := EntryType("carrier-pigeon").EventChannel(); got != string(EntryTypeWhatsApp) {
		t.Errorf("EventChannel() = %q, want the whatsapp fallback", got)
	}
	if got := EntryType("").EventChannel(); got != string(EntryTypeWhatsApp) {
		t.Errorf("empty EventChannel() = %q, want the whatsapp fallback", got)
	}
}
