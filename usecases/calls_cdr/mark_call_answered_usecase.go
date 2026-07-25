package calls_cdr_usecase

import (
	"strings"
	"time"

	"vozko/domain/calls/cdr"
)

type markCallAnsweredUseCase struct {
	repo cdr.Repository
}

func NewMarkCallAnsweredUseCase(repo cdr.Repository) cdr.MarkCallAnsweredUseCase {
	return &markCallAnsweredUseCase{repo: repo}
}

func (uc *markCallAnsweredUseCase) Execute(callID string, answeredAt time.Time) error {
	if strings.TrimSpace(callID) == "" {
		return cdr.ErrCallIDRequired
	}
	if answeredAt.IsZero() {
		answeredAt = time.Now().UTC()
	}
	return uc.repo.MarkAnswered(callID, answeredAt)
}
