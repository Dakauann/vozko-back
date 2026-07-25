package calls_cdr_usecase

import (
	"strings"
	"time"

	"vozko/domain/calls/cdr"
)

type completeCallUseCase struct {
	repo cdr.Repository
}

func NewCompleteCallUseCase(repo cdr.Repository) cdr.CompleteCallUseCase {
	return &completeCallUseCase{repo: repo}
}

func (uc *completeCallUseCase) Execute(input cdr.CompleteCallInput) error {
	if strings.TrimSpace(input.CallID) == "" {
		return cdr.ErrCallIDRequired
	}

	endedAt := input.EndedAt
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}

	existing, err := uc.repo.GetByCallID(input.CallID)
	if err != nil {
		return err
	}
	if existing.IsTerminal() {
		return cdr.ErrAlreadyCompleted
	}

	status := input.Status
	if status == "" {
		status = cdr.StatusCompleted
	}

	durationSec := 0
	if !existing.StartedAt.IsZero() && endedAt.After(existing.StartedAt) {
		durationSec = int(endedAt.Sub(existing.StartedAt).Seconds())
	}

	return uc.repo.Complete(cdr.CompleteInput{
		CallID:      input.CallID,
		Status:      status,
		EndedAt:     &endedAt,
		DurationSec: durationSec,
		EndReason:   input.EndReason,
	})
}
