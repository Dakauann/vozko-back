package billing

import (
	"errors"
	"testing"
	"time"
)

func TestResolveLocation_UsesLoadedWhenAvailable(t *testing.T) {
	want := time.FixedZone("LOADED", -2*60*60)
	got := resolveLocation(func(string) (*time.Location, error) { return want, nil }, "X", time.UTC)
	if got != want {
		t.Fatalf("expected the loaded location, got %v", got)
	}
}

func TestResolveLocation_FallbackWhenLoadFails(t *testing.T) {
	fallback := time.FixedZone("FB", -3*60*60)
	got := resolveLocation(func(string) (*time.Location, error) { return nil, errors.New("no tzdata") }, "America/Sao_Paulo", fallback)
	if got != fallback {
		t.Fatalf("expected the fallback location when load fails, got %v", got)
	}
}

func TestLocationBRT_IsUTCMinus3(t *testing.T) {
	loc := LocationBRT()
	if loc == nil {
		t.Fatal("LocationBRT must never be nil")
	}
	// A fixed instant, viewed in the billing zone, must read as UTC-3 (Brazil has no DST since 2019).
	ref := time.Date(2026, time.June, 30, 15, 0, 0, 0, time.UTC).In(loc)
	if _, offset := ref.Zone(); offset != -3*60*60 {
		t.Fatalf("billing zone offset = %d seconds, want -10800 (UTC-3)", offset)
	}
	// The same instant is 12:00 local.
	if ref.Hour() != 12 {
		t.Fatalf("15:00 UTC should be 12:00 in BRT, got %02d:00", ref.Hour())
	}
}
