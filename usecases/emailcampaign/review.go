package emailcampaign_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/balance"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/domain/selection"
	"vozko/domain/shared"
	balance_usecase "vozko/usecases/balance"
)

type Quote struct {
	Units          int64 `json:"units"`
	UnitMicros     int64 `json:"unit_micros"`
	UnitCostMicros int64 `json:"unit_cost_micros"`
	TotalMicros    int64 `json:"total_micros"`
}

func quoteOf(q balance_usecase.EmailQuote) Quote {
	return Quote{Units: q.Units, UnitMicros: q.UnitMicros, UnitCostMicros: q.UnitCostMicros, TotalMicros: q.TotalMicros}
}

func (q Quote) forUnits(units int64) Quote {
	return Quote{Units: units, UnitMicros: q.UnitMicros, UnitCostMicros: q.UnitCostMicros, TotalMicros: units * q.UnitMicros}
}

type PricedQuote struct {
	Quote
	UnitBRL  float64
	TotalBRL float64
}

type Review struct {
	Audience              emailcampaign.AudienceSummary
	Send                  Quote
	FirstBatch            Quote
	BatchSize             int64
	BalanceMicros         int64
	MissingMicros         int64
	AfterFirstBatchMicros int64
	TestBatch             int
	Check                 emailcampaign.LaunchReport
}

type ReviewView struct {
	Review
	Send               PricedQuote
	FirstBatch         PricedQuote
	ExchangeRate       float64
	BalanceBRL         float64
	MissingBRL         float64
	AfterFirstBatchBRL float64
	TimeZone           string
}

func priced(q Quote, rate float64) PricedQuote {
	return PricedQuote{Quote: q, UnitBRL: shared.EstimateBRLFromMicros(q.UnitMicros, rate), TotalBRL: shared.EstimateBRLFromMicros(q.TotalMicros, rate)}
}

type reviewed struct {
	Review
	plan audiencePlan
}

func (s *Service) Review(ctx context.Context, a Actor, campaignID string) (ReviewView, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return ReviewView{}, err
	}
	r, err := s.review(ctx, scopeOf(a), *c, nil)
	if err != nil {
		return ReviewView{}, err
	}
	fx, err := balance_usecase.ReadExchangeRate(s.deps.Rates)
	if err != nil {
		return ReviewView{}, err
	}
	return ReviewView{
		Review:             r.Review,
		Send:               priced(r.Send, fx.Rate),
		FirstBatch:         priced(r.FirstBatch, fx.Rate),
		ExchangeRate:       fx.Rate,
		BalanceBRL:         shared.EstimateBRLFromMicros(r.BalanceMicros, fx.Rate),
		MissingBRL:         shared.EstimateBRLFromMicros(r.MissingMicros, fx.Rate),
		AfterFirstBatchBRL: shared.EstimateBRLFromMicros(r.AfterFirstBatchMicros, fx.Rate),
		TimeZone:           emailsending.TimeZoneName,
	}, nil
}

func (s *Service) review(ctx context.Context, scope selection.Scope, c emailcampaign.Campaign, scheduledAt *time.Time) (reviewed, error) {
	plan, err := s.planAudience(ctx, scope, c)
	if err != nil {
		return reviewed{}, err
	}
	out := reviewed{plan: plan, Review: Review{Audience: plan.summary, BatchSize: emailcampaign.BillingBatchSize, TestBatch: emailcampaign.PlanTestBatch(plan.summary.Sendable, plan.summary.NeverMailed)}}
	if units := int64(plan.summary.Sendable); units > 0 {
		q, err := s.deps.Billing.QuoteRun(ctx, c.WorkspaceID, units)
		if err != nil {
			return reviewed{}, err
		}
		out.Send = quoteOf(q)
		out.FirstBatch = out.Send.forUnits(emailcampaign.FirstBatchUnits(units))
	}
	wallet, err := s.deps.Wallet.GetByWorkspaceID(c.WorkspaceID)
	switch {
	case errors.Is(err, balance.ErrBalanceNotFound):
	case err != nil:
		return reviewed{}, err
	case wallet != nil:
		out.BalanceMicros = wallet.Amount
	}
	out.MissingMicros = max(out.FirstBatch.TotalMicros-out.BalanceMicros, 0)
	out.AfterFirstBatchMicros = max(out.BalanceMicros-out.FirstBatch.TotalMicros, 0)
	sender, err := s.senderState(ctx, c)
	if err != nil {
		return reviewed{}, err
	}
	out.Check = emailcampaign.LaunchCheck(emailcampaign.LaunchInput{
		Now:           s.now(),
		Sender:        sender,
		Document:      c.RenderDocument(),
		Subject:       c.Subject,
		ABTest:        c.ABTest,
		Audience:      plan.summary,
		MissingMicros: out.MissingMicros,
		ScheduledAt:   scheduledAt,
	})
	return out, nil
}

func (s *Service) senderState(ctx context.Context, c emailcampaign.Campaign) (emailcampaign.SenderState, error) {
	account, err := s.deps.Accounts.GetByWorkspace(ctx, c.WorkspaceID)
	if missingSender(err) || (err == nil && account == nil) {
		return emailcampaign.SenderMissing, nil
	}
	if err != nil {
		return "", err
	}
	switch account.CanSendOnDemand() {
	case nil:
	case emailsending.ErrAccountPaused:
		return emailcampaign.SenderAccountPaused, nil
	default:
		return emailcampaign.SenderMissing, nil
	}
	var sender *emailsending.Sender
	if c.SenderID != "" {
		sender, err = s.deps.Senders.Get(ctx, c.WorkspaceID, c.SenderID)
	} else {
		sender, err = s.deps.Senders.GetDefault(ctx, c.WorkspaceID)
	}
	if missingSender(err) || (err == nil && sender == nil) {
		return emailcampaign.SenderMissing, nil
	}
	if err != nil {
		return "", err
	}
	sendingDomain, err := s.deps.Domains.Get(ctx, c.WorkspaceID, sender.DomainID)
	if missingSender(err) || (err == nil && sendingDomain == nil) {
		return emailcampaign.SenderNotVerified, nil
	}
	if err != nil {
		return "", err
	}
	if sender.CanSendFrom(*sendingDomain) != nil {
		return emailcampaign.SenderNotVerified, nil
	}
	return emailcampaign.SenderReady, nil
}

func missingSender(err error) bool {
	return errors.Is(err, emailsending.ErrSenderNotFound) || errors.Is(err, emailsending.ErrNoDefaultSender) ||
		errors.Is(err, emailsending.ErrAccountNotFound) || errors.Is(err, emailsending.ErrAccountNotProvisioned) ||
		errors.Is(err, emailsending.ErrDomainNotFound)
}
