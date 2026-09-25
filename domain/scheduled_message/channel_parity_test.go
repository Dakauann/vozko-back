package scheduled_message

import (
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestEveryViewableChannelCanBeScheduledTo(t *testing.T) {
	for _, entryType := range shared.ConversationViewableEntryTypes() {
		m := &ScheduledMessage{
			WorkspaceID:     "ws-1",
			EntryID:         "entry-1",
			EntryType:       entryType,
			CreatedByUserID: "user-1",
			Text:            "oi",
			ScheduledAt:     time.Now().Add(time.Hour),
		}
		m.Normalize()

		if err := m.Validate(); err != nil {
			t.Errorf("%q is a viewable conversation but cannot carry a scheduled message: %v",
				entryType, err)
		}
	}
}

func TestTheWindowRuleAnswersForEveryChannelShape(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	expires := now.Add(6 * time.Hour)

	shapes := []struct {
		name      string
		channels  string
		open      bool
		expiresAt *time.Time
		wantBound bool
	}{
		{
			name:      "a clock that is running",
			channels:  "whatsapp, instagram, telegram in business mode",
			open:      true,
			expiresAt: &expires,
			wantBound: true,
		},
		{
			name:      "no clock at all",
			channels:  "telegram in bot mode, a healthy linked device",
			open:      true,
			expiresAt: nil,
			wantBound: true,
		},
		{
			name:      "structurally blocked, no clock",
			channels:  "a blocked bot, a revoked reply right, a dead session",
			open:      false,
			expiresAt: nil,
			wantBound: false,
		},
		{
			name:      "structurally blocked, with a countdown",
			channels:  "unofficial whatsapp under a provider restriction",
			open:      false,
			expiresAt: &expires,
			wantBound: false,
		},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			_, err := KindText.LatestAllowed(shape.open, shape.expiresAt, now)
			if shape.wantBound && err != nil {
				t.Errorf("%s (%s) produced no bound: %v", shape.name, shape.channels, err)
			}
			if !shape.wantBound && err == nil {
				t.Errorf("%s (%s) produced a bound; scheduling would be offered where sending is impossible",
					shape.name, shape.channels)
			}
		})
	}
}
