package whatsapp_outreach

import (
	"context"
	"fmt"
	"time"

	"vozko/domain/cache"
	workspace_config "vozko/domain/workspace_config"
)

// configSpamPolicy reads the workspace's cooldown from its config.
//
// An adapter rather than the repository itself, so this package depends on one
// integer instead of on every setting a workspace has.
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

// sharedStateLimiter is a fixed-window counter in shared state.
//
// Shared rather than in-process because the cap has to hold across replicas: a
// per-process counter would multiply the ceiling by however many servers happen
// to be running, which is the one number nobody deploying a server thinks about.
//
// A fixed window rather than a sliding one on purpose. This is a guard against
// somebody scripting the endpoint, not a fairness mechanism, and the failure
// mode of a fixed window — a burst across a boundary — is at most twice a cap
// that is already generous for a human.
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
		// Only the window's first writer sets the expiry; re-setting it on every
		// call would slide the window forward forever and the counter would never
		// reset.
		if _, expireErr := l.state.Expire(key, window+time.Minute); expireErr != nil {
			return true, expireErr
		}
	}
	return count <= int64(limit), nil
}
