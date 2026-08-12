package scheduled_message

import (
	"testing"
	"time"

	"vozko/domain/shared"
)

// Scheduling joins the parity discipline in domain/shared/channel_parity_test.go,
// for the same reason those guards exist: every feature that ever asked
// `entryType == "whatsapp"` silently lost a channel, and nothing reported it.
//
// This feature is channel-agnostic by construction — it reads the window through
// the port every channel already answers and sends through the use case the live
// composer uses — so these are cheap to keep true. They are here to make it
// LOUD if that ever stops being the case.

// Any conversation an operator can open, they can schedule into. There is no
// per-channel branch anywhere in the feature, so a channel that is viewable but
// not schedulable would mean someone added one.
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

// The window rule must produce an answer for every channel, not just the ones
// with a 24-hour clock. A channel whose window state fell through to "no bound"
// would either refuse every schedule or accept an unbounded one.
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
			_, err := LatestAllowed(shape.open, shape.expiresAt, now)
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
