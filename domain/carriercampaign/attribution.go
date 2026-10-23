package carriercampaign

import (
	"time"

	"vozko/domain/carriermessaging"
)

const day = 24 * time.Hour

type Touch struct {
	MessageID   string
	CampaignID  string
	RunID       string
	Variant     int
	DeliveredAt time.Time
}

func Attributes(deliveredAt, wonAt time.Time, days int) bool {
	if deliveredAt.IsZero() || days < 1 || days > carriermessaging.MaxAttributionDays {
		return false
	}
	return wonAt.After(deliveredAt) && !wonAt.After(deliveredAt.Add(time.Duration(days)*day))
}

func LastTouch(touches []Touch, wonAt time.Time, days int) (Touch, bool) {
	var best Touch
	found := false
	for _, t := range touches {
		if !Attributes(t.DeliveredAt, wonAt, days) {
			continue
		}
		if !found || t.DeliveredAt.After(best.DeliveredAt) || (t.DeliveredAt.Equal(best.DeliveredAt) && t.MessageID > best.MessageID) {
			best, found = t, true
		}
	}
	return best, found
}
