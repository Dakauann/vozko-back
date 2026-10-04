package advertising

import (
	"context"

	ads "vozko/domain/advertising"
)

type subscriptionGateway interface {
	AppSubscriptions(ctx context.Context) ([]ads.AppSubscription, error)
	SubscribeApp(ctx context.Context, sub ads.AppSubscription, verifyToken string) error
}

type WebhookSubscriptionUseCase struct {
	gateway     subscriptionGateway
	want        ads.AppSubscription
	verifyToken string
}

func NewWebhookSubscriptionUseCase(gateway subscriptionGateway, callbackURL, verifyToken string) *WebhookSubscriptionUseCase {
	return &WebhookSubscriptionUseCase{gateway: gateway, want: ads.AccountWebhook(callbackURL), verifyToken: verifyToken}
}

func (uc *WebhookSubscriptionUseCase) Ensure(ctx context.Context) (bool, error) {
	current, err := uc.gateway.AppSubscriptions(ctx)
	if err != nil {
		return false, err
	}
	if uc.want.CoveredBy(current) {
		return false, nil
	}
	if err := uc.gateway.SubscribeApp(ctx, uc.want, uc.verifyToken); err != nil {
		return false, err
	}
	current, err = uc.gateway.AppSubscriptions(ctx)
	if err != nil {
		return false, err
	}
	if !uc.want.CoveredBy(current) {
		return false, ads.ErrWebhookNotSubscribed
	}
	return true, nil
}
