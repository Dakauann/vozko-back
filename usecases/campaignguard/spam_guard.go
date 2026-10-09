package campaignguard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	lcs "vozko/domain/lead_campaign_send"
	"vozko/domain/shared"
	wsc "vozko/domain/workspace_config"
)

const (
	FactsChunk = 5000
	ClaimTTL   = 10 * time.Minute
)

var (
	ErrUnavailable       = errors.New("campaign guard: the send eligibility check is not configured")
	ErrWorkspaceRequired = errors.New("campaign guard: the workspace is required")
	ErrSendClaimed       = errors.New("campaign guard: another send to this contact from this number is in progress")
)

type SendClaims interface {
	SetNX(key string, value string, ttl time.Duration) (bool, error)
	Del(keys ...string) error
}

type SpamPolicy interface {
	SpamProtectionDays(ctx context.Context, workspaceID string) (int, error)
}

type SpamGuard struct {
	policy SpamPolicy
	sends  lcs.Repository
	claims SendClaims
	now    func() time.Time
}

func NewSpamGuard(policy SpamPolicy, sends lcs.Repository, claims SendClaims, now func() time.Time) (*SpamGuard, error) {
	if policy == nil || sends == nil || claims == nil {
		return nil, ErrUnavailable
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SpamGuard{policy: policy, sends: sends, claims: claims, now: now}, nil
}

func (g *SpamGuard) InCooldown(ctx context.Context, workspaceID, leadID, senderID string) (bool, error) {
	days, err := g.days(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	if days <= 0 || strings.TrimSpace(senderID) == "" || strings.TrimSpace(leadID) == "" {
		return false, nil
	}
	lastSent, err := g.sends.GetLastSendTime(leadID, senderID)
	if err != nil {
		return false, fmt.Errorf("campaign guard: could not read the last send to this contact: %w", err)
	}
	return lcs.WithinSpamWindow(lastSent, days, g.now()), nil
}

func (g *SpamGuard) InCooldownMany(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (map[string]bool, error) {
	days, err := g.days(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return g.inCooldownWithin(days, distinct(leadIDs), senderID)
}

func (g *SpamGuard) inCooldownWithin(days int, leadIDs []string, senderID string) (map[string]bool, error) {
	out := map[string]bool{}
	if days <= 0 || strings.TrimSpace(senderID) == "" || len(leadIDs) == 0 {
		return out, nil
	}
	now := g.now()
	for _, chunk := range chunks(leadIDs, FactsChunk) {
		lastSends, err := g.sends.GetLastSendTimesBatch(chunk, senderID)
		if err != nil {
			return nil, fmt.Errorf("campaign guard: could not read the last sends to these contacts: %w", err)
		}
		for leadID, at := range lastSends {
			stamp := at
			if lcs.WithinSpamWindow(&stamp, days, now) {
				out[leadID] = true
			}
		}
	}
	return out, nil
}

func (g *SpamGuard) Claim(ctx context.Context, workspaceID, leadID, senderID string) (func(), error) {
	days, err := g.days(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	leadID, senderID = strings.TrimSpace(leadID), strings.TrimSpace(senderID)
	if days <= 0 || leadID == "" || senderID == "" {
		return func() {}, nil
	}
	key := "campaign:send-claim:" + senderID + ":" + leadID
	taken, err := g.claims.SetNX(key, workspaceID, ClaimTTL)
	if err != nil {
		return nil, fmt.Errorf("campaign guard: could not claim this contact for the send: %w", err)
	}
	if !taken {
		return nil, ErrSendClaimed
	}
	return func() { _ = g.claims.Del(key) }, nil
}

func (g *SpamGuard) Record(leadID, senderID, campaignID string) error {
	return g.sends.Record(leadID, senderID, campaignID)
}

func (g *SpamGuard) days(ctx context.Context, workspaceID string) (int, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return 0, ErrWorkspaceRequired
	}
	days, err := g.policy.SpamProtectionDays(ctx, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("campaign guard: could not read the spam protection policy: %w", err)
	}
	return days, nil
}

type WorkspaceConfigs interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type configSpamPolicy struct {
	configs WorkspaceConfigs
}

func NewConfigSpamPolicy(configs WorkspaceConfigs) (SpamPolicy, error) {
	if configs == nil {
		return nil, ErrUnavailable
	}
	return &configSpamPolicy{configs: configs}, nil
}

func (p *configSpamPolicy) SpamProtectionDays(ctx context.Context, workspaceID string) (int, error) {
	if p.configs == nil {
		return 0, ErrUnavailable
	}
	cfg, err := p.configs.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	if cfg == nil {
		return 0, ErrUnavailable
	}
	return cfg.CampaignSpamProtectionDays, nil
}

func distinct(ids []string) []string {
	return shared.DistinctTrimmed(ids)
}

func chunks(ids []string, size int) [][]string {
	var out [][]string
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[start:end])
	}
	return out
}
