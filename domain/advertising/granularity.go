package advertising

import (
	"strings"
	"time"
)

type Granularity string

const (
	GranularityDay   Granularity = "day"
	GranularityWeek  Granularity = "week"
	GranularityMonth Granularity = "month"
)

func GranularityOf(raw string) (Granularity, error) {
	switch g := Granularity(strings.ToLower(strings.TrimSpace(raw))); g {
	case "", GranularityDay:
		return GranularityDay, nil
	case GranularityWeek, GranularityMonth:
		return g, nil
	}
	return "", FieldError("granularity", "invalid")
}

func (g Granularity) BucketOf(day time.Time) time.Time {
	switch g {
	case GranularityWeek:
		sinceMonday := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -sinceMonday)
	case GranularityMonth:
		return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
	}
	return day
}
