package quality

import (
	"testing"
	"time"
)

func TestATemplateMetaHasNotRatedYetReadsAsUnknown(t *testing.T) {
	reading := ReadTemplate(map[Signal]Change{})

	if reading.Quality != QualityUnknown || !reading.QualityAt.IsZero() || reading.Flagged {
		t.Fatalf("reading = %+v, want unknown quality and not flagged", reading)
	}
}

func TestTheLatestChangesGiveQualityFlagAndTimes(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	reading := ReadTemplate(map[Signal]Change{
		SignalQuality:  {Current: " yellow ", OccurredAt: at},
		SignalStatus:   {Current: "FLAGGED", OccurredAt: at.Add(time.Hour)},
		SignalCategory: {Current: "MARKETING", OccurredAt: at.Add(2 * time.Hour)},
	})

	if reading.Quality != "YELLOW" || !reading.QualityAt.Equal(at) {
		t.Fatalf("quality = %q at %v", reading.Quality, reading.QualityAt)
	}
	if !reading.Flagged || !reading.StatusAt.Equal(at.Add(time.Hour)) || !reading.CategoryAt.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("reading = %+v", reading)
	}
}

func TestALaterStatusEventClearsTheFlag(t *testing.T) {
	reading := ReadTemplate(map[Signal]Change{SignalStatus: {Current: "REINSTATED", OccurredAt: time.Now()}})

	if reading.Flagged {
		t.Fatal("a status event after FLAGGED clears the flag")
	}
}
