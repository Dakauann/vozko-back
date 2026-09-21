package whatsapp_outreach

import (
	"context"
	"fmt"
	"time"

	"vozko/domain/cache"
	workspace_config "vozko/domain/workspace_config"
)

type configSpamPolicy struct {
	configs workspace_config.Repository
}

func NewConfigSpamPolicy(configs workspace_config.Repository) SpamPolicyReader {
	return &configSpamPolicy{configs: configs}
}

func (p *configSpamPolicy) SpamProtectionDays(ctx context.Context, workspaceID string) (int, error) {
	if p.configs == nil {
		return 0, nil
	}
	cfg, err := p.configs.GetByWorkspaceID(ctx, workspaceID)
	if err != nil || cfg == nil {
		return 0, err
	}
	return cfg.CampaignSpamProtectionDays, nil
}

type sharedStateLimiter struct {
	state cache.SharedState
}

func NewSharedStateLimiter(state cache.SharedState) RateLimiter {
	return &sharedStateLimiter{state: state}
}

func (l *sharedStateLimiter) Allow(_ context.Context, workspaceID string, limit int, window time.Duration) (bool, error) {
	if l.state == nil || limit <= 0 {
		return true, nil
	}
	key := fmt.Sprintf("whatsapp:outreach:rate:%s:%d", workspaceID, time.Now().UTC().Truncate(window).Unix())
	count, err := l.state.IncrBy(key, 1)
	if err != nil {
		return true, err
	}
	if count == 1 {
		if _, expireErr := l.state.Expire(key, window+time.Minute); expireErr != nil {
			return true, expireErr
		}
	}
	return count <= int64(limit), nil
}
