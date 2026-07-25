package calendar_usecase

import (
	"strings"

	"github.com/google/uuid"
)

const googleCalendarClientIDPrefix = "gcal_"

func clientGoogleCalendarEventID(googleEventID string) string {
	return googleCalendarClientIDPrefix + googleEventID
}

func googleCalendarEventIDFromRouteID(eventID string) (string, bool) {
	trimmed := strings.TrimSpace(eventID)
	if trimmed == "" {
		return "", false
	}

	if strings.HasPrefix(trimmed, googleCalendarClientIDPrefix) {
		return strings.TrimPrefix(trimmed, googleCalendarClientIDPrefix), true
	}

	if _, err := uuid.Parse(trimmed); err != nil {
		return trimmed, true
	}

	return "", false
}
