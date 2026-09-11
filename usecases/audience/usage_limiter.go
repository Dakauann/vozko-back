package audience_usecase

import (
	"context"
	"log"
	"strconv"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/cache"
)

// The rolling analysis budget, stored as one hash of hourly buckets per
// workspace.
//
// Shape: key `audience:usage:<workspace>`, field `2006-01-02T15`, value the
// count claimed in that hour. Reading the budget sums the fields inside the
// window; the rest are stale and ignored. The whole key carries a TTL a little
// longer than the window, so a workspace that stops analysing cleans itself up
// without a sweep.
//
// It uses only the hash operations the shared-state port already has, so this
// added no cache primitives and nothing else had to learn about it.

const (
	usageKeyPrefix = "audience:usage:"

	// usageKeyTTL outlives the window so the last bucket is never evicted while
	// it still counts, and expires soon after so an idle workspace leaves
	// nothing behind.
	usageKeyTTL = ca.UsageWindow + 2*time.Hour
)

type usageLimiter struct {
	state cache.SharedState
}

// NewUsageLimiter builds the rolling budget over shared state.
func NewUsageLimiter(state cache.SharedState) ca.UsageLimiter {
	return &usageLimiter{state: state}
}

func usageKey(workspaceID string) string { return usageKeyPrefix + workspaceID }

// Claim records items and reports whether they fit.
//
// Read-then-increment, not a single atomic compare-and-add, and that is a
// deliberate trade rather than an oversight. Two replicas claiming at the same
// instant can both pass a check that only one should have: the overshoot is
// bounded by what they can claim in one pass, at most a batch each, against a
// ceiling in the thousands. The alternative is a server-side script, which
// would put a Redis dialect into a port that today speaks only in operations
// every backend has. A spend ceiling is a guard against a runaway bill, not an
// accounting boundary, and it is read back honestly: Usage reports what was
// actually claimed, including an overshoot, rather than clamping to look tidy.
func (l *usageLimiter) Claim(ctx context.Context, workspaceID string, items, limit int, now time.Time) (bool, error) {
	if l == nil || l.state == nil || items <= 0 {
		return true, nil
	}
	if limit <= 0 {
		limit = ca.DefaultDailyCap
	}

	usage, err := l.Read(ctx, workspaceID, limit, now)
	if err != nil {
		return false, err
	}
	if usage.Used+items > limit {
		return false, nil
	}

	if _, err := l.state.HIncrBy(usageKey(workspaceID), ca.UsageBucketKey(now), int64(items)); err != nil {
		return false, err
	}
	// Best effort: the fields carry their own hour, so a missing TTL leaves a
	// key behind but can never make the window wrong.
	if _, err := l.state.Expire(usageKey(workspaceID), usageKeyTTL); err != nil {
		log.Printf("[audience-usage] could not set TTL for workspace %s: %v", workspaceID, err)
	}
	return true, nil
}

// Release gives back items that were claimed and then never classified.
//
// Returned to the bucket they were most likely claimed in, the current one,
// which is where a batch that fails moments after being planned belongs. The
// bucket is allowed to go negative rather than being floored: clamping here
// would quietly keep part of a refund, and Read floors the total instead, which
// is the one number anybody sees.
func (l *usageLimiter) Release(ctx context.Context, workspaceID string, items int, now time.Time) error {
	if l == nil || l.state == nil || items <= 0 {
		return nil
	}
	_, err := l.state.HIncrBy(usageKey(workspaceID), ca.UsageBucketKey(now), -int64(items))
	return err
}

// Read sums the buckets inside the window.
//
// One round trip for the whole hash: at most 24 live fields plus whatever has
// not expired yet, which is small enough that fetching it beats naming 24
// fields explicitly.
func (l *usageLimiter) Read(_ context.Context, workspaceID string, limit int, now time.Time) (ca.Usage, error) {
	usage := ca.Usage{Limit: limit}
	if usage.Limit <= 0 {
		usage.Limit = ca.DefaultDailyCap
	}
	if l == nil || l.state == nil {
		return usage, nil
	}

	fields, err := l.state.HGetAll(usageKey(workspaceID))
	if err != nil {
		return ca.Usage{}, err
	}

	cutoff := now.UTC().Add(-ca.UsageWindow)
	for bucket, raw := range fields {
		at, err := ca.ParseUsageBucketKey(bucket)
		if err != nil {
			// A field this code did not write. Ignored rather than guessed at.
			continue
		}
		if at.Before(cutoff) {
			// Outside the window: stale, and counting it is exactly the cliff
			// this replaced.
			continue
		}
		count, err := strconv.Atoi(raw)
		if err != nil || count == 0 {
			continue
		}
		usage.Used += count
		if count > 0 && (usage.OldestAt.IsZero() || at.Before(usage.OldestAt)) {
			usage.OldestAt = at
		}
	}
	// A refund landing in an hour with nothing else in it can drive the sum
	// below zero. Nobody is owed budget, so the floor is here rather than in
	// every caller.
	if usage.Used < 0 {
		usage.Used = 0
	}
	return usage, nil
}
