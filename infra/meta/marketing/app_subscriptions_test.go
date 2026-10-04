package marketing

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/advertising"
)

func TestAppSubscriptionsReadsEachObjectWithItsFields(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"object":"ad_account","callback_url":"https://api/x","active":true,"fields":[{"name":"effective_status","version":"v26.0"},{"name":"creative_fatigue"}]},{"object":"page","callback_url":"https://api/y","active":true,"fields":[{"name":"leadgen"}]}]}`))
	g.appID = "app-1"
	subs, err := g.AppSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs[0].Object != "ad_account" || len(subs[0].Fields) != 2 || subs[0].Fields[0] != "effective_status" || (*calls)[0].path != "/v26.0/app-1/subscriptions" {
		t.Fatalf("subs %+v calls %+v", subs, *calls)
	}
}

func TestSubscribeAppSendsTheWholeFieldListWithTheVerifyToken(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"success":true}`))
	g.appID = "app-1"
	if err := g.SubscribeApp(context.Background(), advertising.AccountWebhook("https://api/x"), "verify"); err != nil {
		t.Fatal(err)
	}
	form := (*calls)[0].form
	if (*calls)[0].method != "POST" || form.Get("object") != "ad_account" || form.Get("callback_url") != "https://api/x" || form.Get("verify_token") != "verify" || form.Get("fields") == "" {
		t.Fatalf("call %+v", (*calls)[0])
	}
}

func TestSubscribeAppTreatsARefusalAsAFailure(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"success":false}`))
	g.appID = "app-1"
	if err := g.SubscribeApp(context.Background(), advertising.AccountWebhook("https://api/x"), "verify"); !errors.Is(err, advertising.ErrWebhookNotSubscribed) {
		t.Fatalf("err %v", err)
	}
}

func TestSubscriptionsNeedTheAppCredentials(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{}`))
	if _, err := g.AppSubscriptions(context.Background()); !errors.Is(err, errAppCredentialsMissing) || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}
