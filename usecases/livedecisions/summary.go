package livedecisions_usecase

import (
	"context"
	"time"

	ld "vozko/domain/livedecision"
)

const MaxSummaryDays = 90

func (s *Service) Summarize(ctx context.Context, days int) ([]ld.Summary, error) {
	if days < 1 {
		days = 1
	}
	if days > MaxSummaryDays {
		days = MaxSummaryDays
	}
	return s.deps.Log.Summarize(ctx, s.deps.Clock().Add(-time.Duration(days)*24*time.Hour))
}
