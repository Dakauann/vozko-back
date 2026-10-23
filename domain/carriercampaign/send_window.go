package carriercampaign

import (
	"errors"
	"fmt"
	"time"

	"vozko/domain/working_hours"
)

var ErrQuietHoursRequired = errors.New("carriercampaign: marketing sends need quiet hours")

func SendWindow(kind Kind, quiet *working_hours.Schedule, now time.Time) (bool, time.Time, error) {
	if !kind.Valid() {
		return false, time.Time{}, ErrKindInvalid
	}
	if !kind.ObeysMarketingRules() {
		return true, now, nil
	}
	if quiet == nil {
		return false, time.Time{}, ErrQuietHoursRequired
	}
	if err := quiet.Validate(); err != nil {
		return false, time.Time{}, fmt.Errorf("%w: %w", ErrQuietHoursRequired, err)
	}
	next, ok := quiet.NextOpen(now)
	if !ok {
		return false, time.Time{}, fmt.Errorf("%w: %w", ErrQuietHoursRequired, working_hours.ErrNoOpenTime)
	}
	if !next.After(now) {
		return true, now, nil
	}
	return false, next, nil
}
