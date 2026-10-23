package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"vozko/domain/cache"
	es "vozko/domain/emailsending"
)

const (
	sendWindowPrefix      = "email:ses:send:"
	sendWindowTTL         = 10 * time.Second
	storeDownRetryAfter   = time.Minute
	localSendSpacing      = time.Second / es.SESSandboxMaxSendRate
	localSendPoll         = 100 * time.Millisecond
	pacerStoreAlertEvery  = time.Minute
	pacingDetailPrefix    = "ses pacing: "
	detailRateReached     = "the SES sending rate of this second is used"
	detailPriorityTaken   = "priority sends asked for the SES sending rate of this second"
	detailFairShareUsed   = "this workspace used its fair share of the SES sending rate"
	detailDailyQuotaUsed  = "the SES 24 hour sending quota is used"
	detailQuotaStale      = "the last SES quota read is too old to pace campaigns"
	detailStoreDown       = "the shared pacing store is unavailable"
	detailLocalSlotMissed = "the local SES sending slot did not free in time"
)

type PacerDeps struct {
	State SendState
	Ops   OpsAlerter
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

type SendPacer struct {
	state  SendState
	ops    OpsAlerter
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
	local  *cache.LocalSpacing
	alerts *cache.LocalSpacing
}

var _ es.SendPacer = (*SendPacer)(nil)

func NewSendPacer(d PacerDeps) (*SendPacer, error) {
	if d.State == nil || d.Ops == nil {
		return nil, errors.New("emailsending: the send pacer needs its shared store and ops alerts")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Sleep == nil {
		d.Sleep = sleepContext
	}
	return &SendPacer{
		state:  d.State,
		ops:    d.Ops,
		now:    d.Now,
		sleep:  d.Sleep,
		local:  cache.NewLocalSpacing(localSendSpacing),
		alerts: cache.NewLocalSpacing(pacerStoreAlertEvery),
	}, nil
}

func (p *SendPacer) Admit(ctx context.Context, ticket es.SendTicket) error {
	lane := es.SendLaneOf(ticket.RunKind)
	now := p.now()
	quota, err := loadSendQuota(p.state, now)
	if err != nil {
		return p.storeDown(ctx, lane, err)
	}
	if lane == es.SendLaneCampaign && !quota.Fresh(now) {
		return throttled(es.DailyQuotaRetryAfter, detailQuotaStale)
	}
	sent, err := sentSince(p.state, quota.ReadAt, now)
	if err != nil {
		return p.storeDown(ctx, lane, err)
	}
	if !quota.DailyRoom(lane, sent) {
		return throttled(es.DailyQuotaRetryAfter, detailDailyQuotaUsed)
	}
	if lane == es.SendLanePriority {
		return p.admitPriority(ctx, quota)
	}
	return p.admitCampaign(ctx, ticket.WorkspaceID, quota, now)
}

func (p *SendPacer) admitPriority(ctx context.Context, quota es.SendQuota) error {
	rate := quota.TokensPerSecond()
	deadline := p.now().Add(es.PriorityTokenWait)
	for {
		now := p.now()
		window := es.SendWindow(now)
		granted, err := p.state.TryIncrBy(totalKey(window), 1, rate)
		if err != nil {
			return p.storeDown(ctx, es.SendLanePriority, err)
		}
		if granted {
			return p.count(ctx, es.SendLanePriority, now)
		}
		if _, err := p.state.IncrWithTTL(priorityWantedKey(window+1), sendWindowTTL); err != nil {
			return p.storeDown(ctx, es.SendLanePriority, err)
		}
		wait := es.UntilNextSendWindow(now)
		if now.Add(wait).After(deadline) {
			return throttled(wait, detailRateReached)
		}
		if err := p.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (p *SendPacer) admitCampaign(ctx context.Context, workspaceID string, quota es.SendQuota, now time.Time) error {
	window := es.SendWindow(now)
	retry := es.UntilNextSendWindow(now)
	wanted, err := readCount(p.state, priorityWantedKey(window))
	if err != nil {
		return p.storeDown(ctx, es.SendLaneCampaign, err)
	}
	budget := es.CampaignTokens(quota.TokensPerSecond(), wanted)
	if budget <= 0 {
		return throttled(retry, detailPriorityTaken)
	}
	members, err := p.waitingCampaigns(window, workspaceID)
	if err != nil {
		return p.storeDown(ctx, es.SendLaneCampaign, err)
	}
	share := es.FairShare(budget, members, workspaceID, window)
	if share <= 0 {
		return throttled(retry, detailFairShareUsed)
	}
	mine := workspaceTokensKey(window, workspaceID)
	granted, err := p.state.TryIncrBy(mine, 1, share)
	if err != nil {
		return p.storeDown(ctx, es.SendLaneCampaign, err)
	}
	if !granted {
		return throttled(retry, detailFairShareUsed)
	}
	granted, err = p.state.TryIncrBy(totalKey(window), 1, budget)
	if err != nil || !granted {
		p.giveBack(mine)
		if err != nil {
			return p.storeDown(ctx, es.SendLaneCampaign, err)
		}
		return throttled(retry, detailRateReached)
	}
	return p.count(ctx, es.SendLaneCampaign, now)
}

func (p *SendPacer) waitingCampaigns(window int64, workspaceID string) ([]string, error) {
	key := campaignWorkspacesKey(window)
	if err := p.state.SAdd(key, workspaceID); err != nil {
		return nil, err
	}
	if _, err := p.state.Expire(key, sendWindowTTL); err != nil {
		return nil, err
	}
	current, err := p.state.SMembers(key)
	if err != nil {
		return nil, err
	}
	previous, err := p.state.SMembers(campaignWorkspacesKey(window - 1))
	if err != nil {
		return nil, err
	}
	return append(current, previous...), nil
}

func (p *SendPacer) count(ctx context.Context, lane es.SendLane, at time.Time) error {
	if err := countSent(p.state, at); err != nil {
		return p.storeDown(ctx, lane, err)
	}
	return nil
}

func (p *SendPacer) giveBack(key string) {
	if _, err := p.state.DecrBy(key, 1); err != nil {
		log.Printf("[emailsending] pacing token %s was not given back (the workspace sends less this second): %v", key, err)
	}
}

func (p *SendPacer) storeDown(ctx context.Context, lane es.SendLane, cause error) error {
	p.alertStore(ctx, cause)
	if lane == es.SendLaneCampaign {
		return throttled(storeDownRetryAfter, detailStoreDown)
	}
	deadline := p.now().Add(es.PriorityTokenWait)
	for {
		now := p.now()
		if p.local.Take(now) {
			return nil
		}
		if now.Add(localSendPoll).After(deadline) {
			return throttled(localSendSpacing, detailLocalSlotMissed)
		}
		if err := p.sleep(ctx, localSendPoll); err != nil {
			return err
		}
	}
}

func (p *SendPacer) alertStore(ctx context.Context, cause error) {
	if !p.alerts.Take(p.now()) {
		return
	}
	detail := fmt.Sprintf("campanhas de e-mail aguardam e envios prioritários saem a 1 por segundo por réplica até o Redis voltar: %v", cause)
	log.Printf("[emailsending] send pacing store unavailable: %s", detail)
	if err := p.ops.Alert(ctx, "Ritmo de envio do SES sem Redis", detail); err != nil {
		log.Printf("[emailsending] ops alert failed: %v", err)
	}
}

func throttled(retryAfter time.Duration, detail string) error {
	return &es.SendError{Kind: es.SendErrorTransient, Detail: pacingDetailPrefix + detail, RetryAfter: retryAfter}
}

func windowKey(kind string, window int64) string {
	return sendWindowPrefix + kind + ":" + strconv.FormatInt(window, 10)
}

func totalKey(window int64) string { return windowKey("total", window) }

func priorityWantedKey(window int64) string { return windowKey("priority_wanted", window) }

func campaignWorkspacesKey(window int64) string { return windowKey("campaigns", window) }

func workspaceTokensKey(window int64, workspaceID string) string {
	return windowKey("workspace", window) + ":" + workspaceID
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
