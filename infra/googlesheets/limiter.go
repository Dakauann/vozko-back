package googlesheets

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/googlesheet"
)

type Quota struct {
	ProjectWritesPerMinute int
	ProjectReadsPerMinute  int
	UserWritesPerMinute    int
	UserReadsPerMinute     int
}

type Limiter struct {
	state cache.SharedState
	quota Quota
	now   func() time.Time
}

var _ googlesheet.Limiter = (*Limiter)(nil)

func NewLimiter(state cache.SharedState, quota Quota) (*Limiter, error) {
	if state == nil {
		return nil, fmt.Errorf("googlesheets: the limiter needs shared state")
	}
	if quota.ProjectWritesPerMinute <= 0 || quota.ProjectReadsPerMinute <= 0 || quota.UserWritesPerMinute <= 0 || quota.UserReadsPerMinute <= 0 {
		return nil, fmt.Errorf("googlesheets: every quota must be positive, got %+v", quota)
	}
	return &Limiter{state: state, quota: quota, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (l *Limiter) minute() string {
	return strconv.FormatInt(l.now().Unix()/60, 10)
}

func (l *Limiter) TakeWrite(_ context.Context, accountKey string) (bool, error) {
	return l.take("w", accountKey, l.quota.ProjectWritesPerMinute, l.quota.UserWritesPerMinute)
}

func (l *Limiter) TakeRead(_ context.Context, accountKey string) (bool, error) {
	return l.take("r", accountKey, l.quota.ProjectReadsPerMinute, l.quota.UserReadsPerMinute)
}

func (l *Limiter) take(kind, accountKey string, projectCap, userCap int) (bool, error) {
	minute := l.minute()
	project := "gs:" + kind + ":project:" + minute
	granted, err := l.state.TryIncrBy(project, 1, int64(projectCap))
	if err != nil || !granted {
		return false, err
	}
	account := "gs:" + kind + ":acct:" + strings.ToLower(strings.TrimSpace(accountKey)) + ":" + minute
	granted, err = l.state.TryIncrBy(account, 1, int64(userCap))
	if err == nil && granted {
		return true, nil
	}
	if _, giveBack := l.state.DecrBy(project, 1); giveBack != nil && err == nil {
		err = giveBack
	}
	return false, err
}

func (l *Limiter) WritePressure(_ context.Context) (float64, error) {
	raw, err := l.state.GetString("gs:w:project:" + l.minute())
	if err != nil {
		return 0, err
	}
	used, _ := strconv.Atoi(strings.TrimSpace(raw))
	return float64(used) / float64(l.quota.ProjectWritesPerMinute), nil
}
