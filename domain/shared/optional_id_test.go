package shared

import "testing"

func TestAnAbsentValueStaysAbsent(t *testing.T) {
	if got := OptionalID(nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestAnEmptyValueBecomesAbsent(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n"} {
		if got := OptionalIDOf(raw); got != nil {
			t.Fatalf("%q resolved to %q; an empty id means no id", raw, *got)
		}
	}
}

func TestARealIDIsTrimmedNotDropped(t *testing.T) {
	got := OptionalIDOf("  9f1d2c3b-4a5e  ")
	if got == nil {
		t.Fatal("a real id was dropped")
	}
	if *got != "9f1d2c3b-4a5e" {
		t.Fatalf("got %q, want the trimmed id", *got)
	}
}

func TestTheCallersValueIsNotMutated(t *testing.T) {
	original := "  abc  "
	got := OptionalID(&original)

	if original != "  abc  " {
		t.Fatalf("caller's string became %q", original)
	}
	if got == nil || *got != "abc" {
		t.Fatalf("got %v", got)
	}
}
