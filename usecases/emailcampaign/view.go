package emailcampaign_usecase

import (
	"context"

	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
	balance_usecase "vozko/usecases/balance"
)

type BillingMoney struct {
	ExchangeRate     float64
	ChargedBRL       float64
	RefundedBRL      float64
	FinalBRL         float64
	RefundPendingBRL float64
}

type CampaignView struct {
	Campaign emailcampaign.Campaign
	Actions  []emailcampaign.Command
	Hold     *emailcampaign.Hold
	Billing  emailcampaign.Billing
	Money    *BillingMoney
	TimeZone string
}

func (s *Service) Present(ctx context.Context, c emailcampaign.Campaign) (CampaignView, error) {
	billing, err := s.billingOf(ctx, c)
	if err != nil {
		return CampaignView{}, err
	}
	reason, err := s.accountStatusReason(ctx, c)
	if err != nil {
		return CampaignView{}, err
	}
	view := CampaignView{
		Campaign: c,
		Actions:  c.Actions(),
		Hold:     c.Hold(s.now(), reason),
		Billing:  billing,
		TimeZone: emailsending.TimeZoneName,
	}
	if fx, err := balance_usecase.ReadExchangeRate(s.deps.Rates); err == nil {
		view.Money = billingMoney(view.Billing, fx.Rate)
	} else {
		s.logf("campaign %s is shown without reais: %v", c.ID, err)
	}
	return view, nil
}

func (s *Service) billingOf(ctx context.Context, c emailcampaign.Campaign) (emailcampaign.Billing, error) {
	if !c.Status.Launched() {
		return emailcampaign.NewBilling(emailcampaign.BillingState{}, 0, nil), nil
	}
	state, err := s.billing.State(ctx, c.ID)
	if err != nil {
		return emailcampaign.Billing{}, err
	}
	counts, err := s.deps.Recipients.Counts(ctx, c.ID)
	if err != nil {
		return emailcampaign.Billing{}, err
	}
	return emailcampaign.NewBilling(state, counts.Sent, c.Settlement), nil
}

func (s *Service) accountStatusReason(ctx context.Context, c emailcampaign.Campaign) (string, error) {
	if c.HoldReason != emailcampaign.HoldAccountPaused {
		return "", nil
	}
	account, err := s.deps.Accounts.GetByWorkspace(ctx, c.WorkspaceID)
	if missingSender(err) || (err == nil && account == nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return account.StatusReason, nil
}

func billingMoney(b emailcampaign.Billing, rate float64) *BillingMoney {
	return &BillingMoney{
		ExchangeRate:     rate,
		ChargedBRL:       shared.BRLFromMicros(b.ChargedMicros, rate),
		RefundedBRL:      shared.BRLFromMicros(b.RefundedMicros, rate),
		FinalBRL:         shared.BRLFromMicros(b.FinalMicros, rate),
		RefundPendingBRL: shared.EstimateBRLFromMicros(b.RefundPendingMicros, rate),
	}
}
