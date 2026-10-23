package googlesheet

import (
	"context"
	"time"

	"vozko/domain/googlesheet"
)

const (
	WrittenRowRetention = 30 * 24 * time.Hour
	purgeBatch          = 5000
	purgeRounds         = 40
)

type Retention struct {
	rows googlesheet.RowRepository
	now  Clock
}

func NewRetention(rows googlesheet.RowRepository) *Retention {
	if rows == nil {
		panic("googlesheet: retention requires rows")
	}
	return &Retention{rows: rows, now: utcNow}
}

func (r *Retention) Execute(ctx context.Context) error {
	before := r.now().Add(-WrittenRowRetention)
	for round := 0; round < purgeRounds; round++ {
		purged, err := r.rows.PurgeWritten(ctx, before, purgeBatch)
		if err != nil || purged < purgeBatch {
			return err
		}
	}
	return nil
}
