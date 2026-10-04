package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
)

const callbackURL = "https://api.vozkoia.com/webhooks/meta-ads"

type fakeSubscriptions struct {
	current    []ads.AppSubscription
	subscribed []ads.AppSubscription
	keeps      bool
	failWith   error
}

func (f *fakeSubscriptions) AppSubscriptions(context.Context) ([]ads.AppSubscription, error) {
	return f.current, f.failWith
}

func (f *fakeSubscriptions) SubscribeApp(_ context.Context, sub ads.AppSubscription, verifyToken string) error {
	if verifyToken != "verify" {
		return errors.New("wrong verify token")
	}
	f.subscribed = append(f.subscribed, sub)
	if f.keeps {
		f.current = []ads.AppSubscription{sub}
	}
	return nil
}

func TestAnUpToDateSubscriptionIsLeftAlone(t *testing.T) {
	gateway := &fakeSubscriptions{current: []ads.AppSubscription{ads.AccountWebhook(callbackURL)}}
	changed, err := NewWebhookSubscriptionUseCase(gateway, callbackURL, "verify").Ensure(context.Background())
	if err != nil || changed || len(gateway.subscribed) != 0 {
		t.Fatalf("changed %v err %v subscribed %d", changed, err, len(gateway.subscribed))
	}
}

func TestAMissingFieldOrCallbackIsSubscribedAndConfirmed(t *testing.T) {
	stale := ads.AccountWebhook(callbackURL)
	stale.Fields = stale.Fields[1:]
	gateway := &fakeSubscriptions{current: []ads.AppSubscription{stale}, keeps: true}
	changed, err := NewWebhookSubscriptionUseCase(gateway, callbackURL, "verify").Ensure(context.Background())
	if err != nil || !changed || len(gateway.subscribed) != 1 || gateway.subscribed[0].CallbackURL != callbackURL {
		t.Fatalf("changed %v err %v subscribed %+v", changed, err, gateway.subscribed)
	}
}

func TestASubscriptionMetaDidNotKeepIsAFailure(t *testing.T) {
	gateway := &fakeSubscriptions{}
	if _, err := NewWebhookSubscriptionUseCase(gateway, callbackURL, "verify").Ensure(context.Background()); !errors.Is(err, ads.ErrWebhookNotSubscribed) {
		t.Fatalf("err %v", err)
	}
}

func TestAnUnreadableSubscriptionListChangesNothing(t *testing.T) {
	gateway := &fakeSubscriptions{failWith: errors.New("meta down")}
	if _, err := NewWebhookSubscriptionUseCase(gateway, callbackURL, "verify").Ensure(context.Background()); err == nil || len(gateway.subscribed) != 0 {
		t.Fatalf("err %v subscribed %d", err, len(gateway.subscribed))
	}
}
