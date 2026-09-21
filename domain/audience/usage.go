package audience

import (
	"context"
	"fmt"
	"time"
)

const (
	UsageWindow = 24 * time.Hour

	UsageBucket = time.Hour
)

type Usage struct {
	Used     int       `json:"used"`
	Limit    int       `json:"limit"`
	OldestAt time.Time `json:"oldestAt,omitempty"`
	Waiting  int       `json:"waiting"`
}

func (u Usage) Constrained() bool {
	return u.Limit > 0 && u.Waiting > u.Remaining()
}

func (u Usage) ClearsIn() time.Duration {
	if u.Limit <= 0 {
		return 0
	}
	overflow := u.Waiting - u.Remaining()
	if overflow <= 0 {
		return 0
	}
	return time.Duration(float64(overflow) / float64(u.Limit) * float64(UsageWindow))
}

func (u Usage) Remaining() int {
	if u.Limit <= 0 {
		return 0
	}
	if u.Used >= u.Limit {
		return 0
	}
	return u.Limit - u.Used
}

func (u Usage) Exhausted() bool {
	return u.Limit > 0 && u.Used >= u.Limit
}

func (u Usage) FreesAt() time.Time {
	if u.OldestAt.IsZero() {
		return time.Time{}
	}
	return u.OldestAt.Add(UsageWindow)
}

func UsageBucketKey(at time.Time) string {
	return at.UTC().Truncate(UsageBucket).Format("2006-01-02T15")
}

func ParseUsageBucketKey(key string) (time.Time, error) {
	at, err := time.Parse("2006-01-02T15", key)
	if err != nil {
		return time.Time{}, fmt.Errorf("audience: bad usage bucket %q: %w", key, err)
	}
	return at.UTC(), nil
}

func UsageBucketsInWindow(now time.Time) []string {
	count := int(UsageWindow / UsageBucket)
	keys := make([]string, 0, count)
	start := now.UTC().Truncate(UsageBucket)
	for i := 0; i < count; i++ {
		keys = append(keys, UsageBucketKey(start.Add(-time.Duration(i)*UsageBucket)))
	}
	return keys
}

type UsageLimiter interface {
	Claim(ctx context.Context, workspaceID string, items, limit int, now time.Time) (bool, error)

	Release(ctx context.Context, workspaceID string, items int, now time.Time) error

	Read(ctx context.Context, workspaceID string, limit int, now time.Time) (Usage, error)
}

type WorkspaceSettings struct {
	DailyCap        int
	DebounceMinutes int
}

type WorkspaceSettingsStore interface {
	Get(ctx context.Context, workspaceID string) (WorkspaceSettings, error)
	Save(ctx context.Context, workspaceID string, settings WorkspaceSettings) error
}

func ResolveDailyCap(workspaceCap, accountCap int) int {
	if workspaceCap > 0 {
		return workspaceCap
	}
	if accountCap > 0 {
		return accountCap
	}
	return DefaultDailyCap
}
