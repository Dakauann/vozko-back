package webhooksubscription_usecase

import (
	"context"

	"vozko/domain/metaplatform"
	"vozko/domain/whatsapp/optout"
)

type Gateway interface {
	AppSubscriptions(ctx context.Context) ([]metaplatform.AppSubscription, error)
	SubscribeApp(ctx context.Context, sub metaplatform.AppSubscription, verifyToken string) error
}

type EnsureUseCase struct {
	gateway     Gateway
	verifyToken string
	fields      []string
}

func NewEnsureUseCase(gateway Gateway, verifyToken string) *EnsureUseCase {
	return &EnsureUseCase{gateway: gateway, verifyToken: verifyToken, fields: []string{optout.WebhookField}}
}

func (uc *EnsureUseCase) Ensure(ctx context.Context) (bool, error) {
	current, err := uc.gateway.AppSubscriptions(ctx)
	if err != nil {
		return false, err
	}
	existing, ok := metaplatform.ActiveSubscription(current, optout.SubscriptionObject)
	if !ok {
		return false, metaplatform.ErrAppSubscriptionMissing
	}
	want := existing.WithFields(uc.fields...)
	if want.CoveredBy(current) {
		return false, nil
	}
	if err := uc.gateway.SubscribeApp(ctx, want, uc.verifyToken); err != nil {
		return false, err
	}
	current, err = uc.gateway.AppSubscriptions(ctx)
	if err != nil {
		return false, err
	}
	if !want.CoveredBy(current) {
		return false, metaplatform.ErrAppSubscriptionNotKept
	}
	return true, nil
}
