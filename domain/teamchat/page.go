package teamchat

import "vozko/domain/shared"

const (
	DefaultMessagePage = 50
	MaxMessagePage     = 100
	DefaultThreadPage  = 30
	MaxThreadPage      = 50
	ScheduledDueBatch  = 200
)

type MessageQuery struct {
	ThreadID  string
	BeforeSeq int64
	AfterSeq  int64
	Limit     int
}

func (q MessageQuery) Validate() error {
	if q.BeforeSeq < 0 || q.AfterSeq < 0 || (q.BeforeSeq > 0 && q.AfterSeq > 0) {
		return ErrPageDirection
	}
	return nil
}

type ThreadQuery struct {
	WorkspaceID string
	UserID      string
	After       *shared.Keyset
	Limit       int
}

func MessagePageLimit(requested int) int {
	return clampPage(requested, DefaultMessagePage, MaxMessagePage)
}

func ThreadPageLimit(requested int) int {
	return clampPage(requested, DefaultThreadPage, MaxThreadPage)
}

func clampPage(requested, fallback, ceiling int) int {
	if requested <= 0 {
		return fallback
	}
	return min(requested, ceiling)
}
