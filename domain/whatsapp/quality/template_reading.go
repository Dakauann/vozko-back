package quality

import (
	"strings"
	"time"
)

const (
	QualityUnknown = "UNKNOWN"
	statusFlagged  = "FLAGGED"
)

type TemplateReading struct {
	Quality    string
	QualityAt  time.Time
	Flagged    bool
	StatusAt   time.Time
	CategoryAt time.Time
}

func ReadTemplate(latest map[Signal]Change) TemplateReading {
	reading := TemplateReading{Quality: QualityUnknown}
	if change, ok := latest[SignalQuality]; ok {
		reading.Quality, reading.QualityAt = normalized(change.Current), change.OccurredAt
	}
	if change, ok := latest[SignalStatus]; ok {
		reading.StatusAt = change.OccurredAt
		reading.Flagged = normalized(change.Current) == statusFlagged
	}
	if change, ok := latest[SignalCategory]; ok {
		reading.CategoryAt = change.OccurredAt
	}
	return reading
}

func normalized(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
