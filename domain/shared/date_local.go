package shared

import (
	"strings"
	"time"
)

var localDateLayouts = []string{
	DateLayout,
	"2/1/2006",
	"2-1-2006",
	"2.1.2006",
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2/1/2006 15:04:05",
	"2/1/2006 15:04",
}

func ParseLocalDate(raw string) (Date, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Date{}, ErrDateInvalid
	}
	for _, layout := range localDateLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return DateOf(parsed), nil
		}
	}
	return Date{}, ErrDateInvalid
}
