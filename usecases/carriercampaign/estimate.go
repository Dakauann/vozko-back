package carriercampaign_usecase

import (
	"context"
	"errors"
	"time"

	audience "vozko/domain/campaignaudience"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

const spendWindow = 30 * 24 * time.Hour

const (
	BlockerSenderNotFound = "sender_not_found"
	BlockerSenderNotReady = "sender_not_ready"
	BlockerRouteNotFound  = "route_not_found"
	BlockerRouteNotReady  = "route_not_ready"
	BlockerRouteInvalid   = "route_invalid"
)

type Readiness struct {
	Ready   bool   `json:"ready"`
	Sender  bool   `json:"sender"`
	Route   bool   `json:"route"`
	Blocker string `json:"blocker,omitempty"`
}

type EstimateResult struct {
	Audience            int             `json:"audience"`
	Count               *audience.Count `json:"count"`
	AudienceFingerprint string          `json:"audience_fingerprint"`
	Estimate            cc.Estimate     `json:"estimate"`
	BalanceMicros       int64           `json:"balance_micros"`
	ShortfallMicros     int64           `json:"shortfall_micros"`
	Readiness           Readiness       `json:"readiness"`
}

var blockers = []struct {
	err  error
	code string
}{
	{rcs.ErrSenderNotFound, BlockerSenderNotFound},
	{rcs.ErrSenderCannotSend, BlockerSenderNotReady},
	{sms.ErrRouteNotFound, BlockerRouteNotFound},
	{sms.ErrRouteCannotSend, BlockerRouteNotReady},
	{sms.ErrRouteInvalid, BlockerRouteInvalid},
	{cc.ErrSMSRouteMismatch, BlockerRouteInvalid},
}

func BlockerOf(err error) (string, bool) {
	for _, b := range blockers {
		if errors.Is(err, b.err) {
			return b.code, true
		}
	}
	return "", false
}

func (s *Service) ready(ctx context.Context, c cc.Campaign) (*sms.Route, error) {
	if err := s.senderReady(ctx, c); err != nil {
		return nil, err
	}
	return s.routeReady(ctx, c)
}

func (s *Service) senderReady(ctx context.Context, c cc.Campaign) error {
	if c.Channel != carriermessaging.ChannelRCS {
		return nil
	}
	sender, err := s.deps.Senders.FindByID(ctx, c.WorkspaceID, c.SenderID)
	if err != nil {
		return err
	}
	if !sender.CanSend() {
		return rcs.ErrSenderCannotSend
	}
	return nil
}

func (s *Service) routeReady(ctx context.Context, c cc.Campaign) (*sms.Route, error) {
	routeID := c.SMSRouteID()
	if routeID == "" {
		return nil, nil
	}
	route, err := s.deps.Routes.FindByID(ctx, c.WorkspaceID, routeID)
	if err != nil {
		return nil, err
	}
	if err := c.ValidateSMSRoute(*route); err != nil {
		return nil, err
	}
	if !route.CanSend() {
		return nil, sms.ErrRouteCannotSend
	}
	return route, nil
}

func (s *Service) readiness(ctx context.Context, c cc.Campaign) (Readiness, *sms.Route, error) {
	senderErr := s.senderReady(ctx, c)
	route, routeErr := s.routeReady(ctx, c)
	r := Readiness{Sender: senderErr == nil, Route: routeErr == nil}
	if c.Channel == carriermessaging.ChannelSMS {
		r.Sender = r.Route
	}
	for _, err := range []error{senderErr, routeErr} {
		if err == nil {
			continue
		}
		code, known := BlockerOf(err)
		if !known {
			return Readiness{}, nil, err
		}
		if r.Blocker == "" {
			r.Blocker = code
		}
	}
	r.Ready = r.Blocker == ""
	if routeErr != nil {
		route = s.routeForQuote(ctx, c)
	}
	return r, route, nil
}

func (s *Service) prices(workspaceID string, channel carriermessaging.Channel) (cc.Prices, error) {
	kinds := carriermessaging.ChargeKindsOf(channel)
	if channel == carriermessaging.ChannelRCS {
		kinds = append(kinds, carriermessaging.ChargeSMSSegment)
	}
	out := cc.Prices{}
	for _, kind := range kinds {
		price, err := s.deps.Prices.Price(workspaceID, kind)
		if errors.Is(err, carriermessaging.ErrPriceUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[kind] = price.PriceMicros
	}
	return out, nil
}

type quote struct {
	audience  int
	estimate  cc.Estimate
	balance   int64
	shortfall int64
}

func (s *Service) quote(ctx context.Context, c cc.Campaign, a Actor, route *sms.Route) (quote, error) {
	if !c.HasAudience() {
		return quote{}, cc.ErrAudienceRequired
	}
	audience, err := s.deps.Selections.Selected(ctx, scopeOf(c.WorkspaceID, a), c.Audience)
	if err != nil {
		return quote{}, err
	}
	prices, err := s.prices(c.WorkspaceID, c.Channel)
	if err != nil {
		return quote{}, err
	}
	estimate, err := c.Estimate(cc.EstimateInput{Recipients: audience, Prices: prices, SMSRoute: route})
	if err != nil {
		return quote{}, err
	}
	balance, err := s.deps.Balance.GetBalance(c.WorkspaceID)
	if err != nil {
		return quote{}, err
	}
	spend, err := s.deps.Spend.SpendSince(ctx, c.WorkspaceID, s.now().Add(-spendWindow))
	if err != nil {
		return quote{}, err
	}
	shortfall, err := estimate.Shortfall(balance, spend, prices)
	if err != nil {
		return quote{}, err
	}
	return quote{audience: audience, estimate: estimate, balance: balance, shortfall: shortfall}, nil
}

func (s *Service) Estimate(ctx context.Context, workspaceID, id string, a Actor) (EstimateResult, error) {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return EstimateResult{}, err
	}
	readiness, route, err := s.readiness(ctx, *c)
	if err != nil {
		return EstimateResult{}, err
	}
	count, err := s.countAudience(ctx, *c, a)
	if err != nil {
		return EstimateResult{}, err
	}
	if route == nil && c.SMSRouteID() != "" {
		result, err := s.audienceOnly(ctx, *c, a, readiness)
		result.Count = count
		return result, err
	}
	q, err := s.quote(ctx, *c, a, route)
	if err != nil {
		return EstimateResult{}, err
	}
	return EstimateResult{Audience: q.audience, Count: count, AudienceFingerprint: c.AudienceFingerprint(), Estimate: q.estimate, BalanceMicros: q.balance, ShortfallMicros: q.shortfall, Readiness: readiness}, nil
}

func (s *Service) audienceOnly(ctx context.Context, c cc.Campaign, a Actor, readiness Readiness) (EstimateResult, error) {
	if !c.HasAudience() {
		return EstimateResult{}, cc.ErrAudienceRequired
	}
	audience, err := s.deps.Selections.Selected(ctx, scopeOf(c.WorkspaceID, a), c.Audience)
	if err != nil {
		return EstimateResult{}, err
	}
	balance, err := s.deps.Balance.GetBalance(c.WorkspaceID)
	if err != nil {
		return EstimateResult{}, err
	}
	return EstimateResult{Audience: audience, AudienceFingerprint: c.AudienceFingerprint(), Estimate: cc.Estimate{Channel: c.Channel, Messages: int64(audience)}, BalanceMicros: balance, Readiness: readiness}, nil
}

func (s *Service) routeForQuote(ctx context.Context, c cc.Campaign) *sms.Route {
	routeID := c.SMSRouteID()
	if routeID == "" {
		return nil
	}
	route, err := s.deps.Routes.FindByID(ctx, c.WorkspaceID, routeID)
	if err != nil {
		return nil
	}
	return route
}
