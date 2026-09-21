package audience_usecase

import (
	"context"
	"log"
	"strconv"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/cache"
)

const (
	usageKeyPrefix = "audience:usage:"

	usageKeyTTL = ca.UsageWindow + 2*time.Hour
)

type usageLimiter struct {
	state cache.SharedState
}

func NewUsageLimiter(state cache.SharedState) ca.UsageLimiter {
	return &usageLimiter{state: state}
}

func usageKey(workspaceID string) string { return usageKeyPrefix + workspaceID }

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
	if _, err := l.state.Expire(usageKey(workspaceID), usageKeyTTL); err != nil {
		log.Printf("[audience-usage] could not set TTL for workspace %s: %v", workspaceID, err)
	}
	return true, nil
}

func (l *usageLimiter) Release(ctx context.Context, workspaceID string, items int, now time.Time) error {
	if l == nil || l.state == nil || items <= 0 {
		return nil
	}
	_, err := l.state.HIncrBy(usageKey(workspaceID), ca.UsageBucketKey(now), -int64(items))
	return err
}

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
			continue
		}
		if at.Before(cutoff) {
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
	if usage.Used < 0 {
		usage.Used = 0
	}
	return usage, nil
}
