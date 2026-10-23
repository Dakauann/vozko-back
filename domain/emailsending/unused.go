package emailsending

import (
	"context"
	"errors"
	"time"
)

const (
	UnusedReleaseAfter  = 30 * 24 * time.Hour
	UnusedReleaseNotice = 7 * 24 * time.Hour
)

var ErrAccountInUse = errors.New("emailsending: the sender account has been used and is kept")

type UnusedStep string

const (
	UnusedKeep    UnusedStep = "keep"
	UnusedWarn    UnusedStep = "warn"
	UnusedRelease UnusedStep = "release"
)

func UnusedReleaseAt(activatedAt time.Time, warnedAt *time.Time) time.Time {
	at := activatedAt.Add(UnusedReleaseAfter)
	if warnedAt != nil {
		if notice := warnedAt.Add(UnusedReleaseNotice); notice.After(at) {
			return notice
		}
	}
	return at
}

func UnusedWarningFrom(activatedAt time.Time) time.Time {
	return activatedAt.Add(UnusedReleaseAfter - UnusedReleaseNotice)
}

func UnusedStepFor(acct SenderAccount, used bool, now time.Time) UnusedStep {
	if used || (acct.Status != AccountActive && acct.Status != AccountDormant) {
		return UnusedKeep
	}
	if acct.UnusedWarnedAt == nil {
		if now.Before(UnusedWarningFrom(acct.CreatedAt)) {
			return UnusedKeep
		}
		return UnusedWarn
	}
	if now.Before(UnusedReleaseAt(acct.CreatedAt, acct.UnusedWarnedAt)) {
		return UnusedWarn
	}
	return UnusedRelease
}

type UnusedAccounts interface {
	ListUnusedCandidates(ctx context.Context, activatedBefore time.Time, afterID string, limit int) ([]SenderAccount, error)
	InUse(ctx context.Context, workspaceID string) (bool, error)
	Forget(ctx context.Context, workspaceID, accountID string) error
}

func UnusedCandidatesActivatedBefore(now time.Time) time.Time {
	return now.Add(-(UnusedReleaseAfter - UnusedReleaseNotice))
}

func UnusedReleaseDay(at time.Time) string {
	return at.In(brasilia).Format("02/01/2006")
}
