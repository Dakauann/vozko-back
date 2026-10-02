package advertising

import (
	"context"
	"slices"

	ads "vozko/domain/advertising"
)

type readinessGateway interface {
	GetBilling(ctx context.Context, token, metaAccountID string, withPaymentMethod bool) (*ads.RemoteBilling, error)
	ListPages(ctx context.Context, token string) ([]ads.RemotePage, error)
	CustomAudienceTermsAccepted(ctx context.Context, token, metaAccountID string) (bool, error)
	ListPixels(ctx context.Context, token, metaAccountID string) ([]ads.Pixel, error)
}

type ReadinessUseCase struct {
	access  accountAccess
	gateway readinessGateway
}

func NewReadinessUseCase(sync *SyncUseCase, gateway readinessGateway) *ReadinessUseCase {
	return &ReadinessUseCase{access: sync.access, gateway: gateway}
}

type Readiness struct {
	Account   *ads.AdAccount
	Checklist ads.AccountReadiness
	Billing   *ads.RemoteBilling
}

func (uc *ReadinessUseCase) Readiness(ctx context.Context, workspaceID, accountID string) (*Readiness, error) {
	account, err := uc.access.accounts.FindByID(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	token, err := uc.access.tokenFor(ctx, account, ads.UseRead)
	if err != nil {
		if account.CanRead() == nil {
			return nil, err
		}
		return &Readiness{Account: account, Checklist: ads.BuildReadiness(account, ads.ReadinessFacts{})}, nil
	}
	facts := uc.observe(ctx, account, token)
	billing := uc.billing(ctx, account, token)
	return &Readiness{Account: account, Checklist: ads.BuildReadiness(account, facts), Billing: billing}, nil
}

func (uc *ReadinessUseCase) billing(ctx context.Context, account *ads.AdAccount, token string) *ads.RemoteBilling {
	if account.CanManage() != nil {
		return nil
	}
	billing, err := uc.gateway.GetBilling(ctx, token, account.MetaAccountID, account.CanChangeBilling() == nil)
	if err != nil {
		uc.access.failed(ctx, account, err)
		return nil
	}
	return billing
}

func (uc *ReadinessUseCase) observe(ctx context.Context, account *ads.AdAccount, token string) ads.ReadinessFacts {
	seen := func(ok bool, err error) ads.Observation {
		if err != nil {
			uc.access.failed(ctx, account, err)
			return ads.Unobserved
		}
		return ads.Observed(ok)
	}
	var facts ads.ReadinessFacts
	pages, err := uc.gateway.ListPages(ctx, token)
	facts.Page = seen(slices.ContainsFunc(pages, func(p ads.RemotePage) bool { return p.CanAdvertise }), err)
	facts.AudienceTerms = seen(uc.gateway.CustomAudienceTermsAccepted(ctx, token, account.MetaAccountID))
	pixels, err := uc.gateway.ListPixels(ctx, token, account.MetaAccountID)
	facts.Pixel = seen(slices.ContainsFunc(pixels, func(p ads.Pixel) bool { return !p.Unavailable }), err)
	return facts
}
