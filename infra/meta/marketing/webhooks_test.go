package marketing

import (
	"context"
	"net/http"
	"testing"
)

func TestSubscribeAccount(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"success":true}`))
	if err := g.SubscribeAccount(context.Background(), "tok", "act_9"); err != nil {
		t.Fatal(err)
	}
	if call := (*calls)[0]; call.method != http.MethodPost || call.path != "/v26.0/act_9/subscribed_apps" {
		t.Fatalf("call = %+v", call)
	}
}

func TestAccountSubscriptionNeedsAcknowledgement(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"success":false}`))
	if err := g.SubscribeAccount(context.Background(), "tok", "9"); err == nil {
		t.Fatal("expected an error")
	}
}
