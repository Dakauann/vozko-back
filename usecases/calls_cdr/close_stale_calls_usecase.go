package calls_cdr_usecase

import (
	"log"
	"time"

	"vozko/domain/calls/cdr"
)

const staleCallsPerSweep = 200

type CloseStaleCallsUseCase struct {
	repo   cdr.Repository
	maxAge time.Duration
	now    func() time.Time
}

func NewCloseStaleCallsUseCase(repo cdr.Repository, maxAge time.Duration, now func() time.Time) *CloseStaleCallsUseCase {
	if now == nil {
		now = time.Now
	}
	return &CloseStaleCallsUseCase{repo: repo, maxAge: maxAge, now: now}
}

func (uc *CloseStaleCallsUseCase) Execute() (int, error) {
	stale, err := uc.repo.ListStale(uc.now().Add(-uc.maxAge), staleCallsPerSweep)
	if err != nil {
		return 0, err
	}
	closed := 0
	reason := cdr.EndReasonInterrupted
	for _, call := range stale {
		endedAt := call.StartedAt
		if call.AnsweredAt != nil {
			endedAt = *call.AnsweredAt
		}
		err := uc.repo.Complete(cdr.CompleteInput{
			CallID:      call.CallID,
			Status:      cdr.CompletionStatus(call.AnsweredAt != nil, reason),
			EndedAt:     &endedAt,
			DurationSec: int(endedAt.Sub(call.StartedAt).Seconds()),
			EndReason:   &reason,
		})
		if err != nil {
			return closed, err
		}
		closed++
	}
	return closed, nil
}

func (uc *CloseStaleCallsUseCase) RunPeriodic(interval time.Duration, logger *log.Logger) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for ; ; <-ticker.C {
			closed, err := uc.Execute()
			switch {
			case err != nil:
				logger.Printf("[CallCDR] closing stale calls failed: %v", err)
			case closed > 0:
				logger.Printf("[CallCDR] closed %d calls left open by an interruption", closed)
			}
		}
	}()
}
