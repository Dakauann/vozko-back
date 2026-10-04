package advertising

import (
	"slices"
	"testing"
)

const testCallback = "https://api.vozkoia.com/webhooks/meta-ads"

func TestTheAccountWebhookAsksForEveryFieldVozkoHandlesOrAcknowledges(t *testing.T) {
	want := AccountWebhook(" " + testCallback + " ")
	if want.Object != "ad_account" || want.CallbackURL != testCallback || !want.Active {
		t.Fatalf("%+v", want)
	}
	for field := range accountFieldKinds {
		if field != "field_changed" && !slices.Contains(want.Fields, field) {
			t.Fatalf("%s is handled but never subscribed", field)
		}
	}
}

func TestASubscriptionIsOnlyRedoneWhenSomethingIsMissing(t *testing.T) {
	want := AccountWebhook(testCallback)
	full := AppSubscription{Object: "ad_account", CallbackURL: testCallback, Fields: append(slices.Clone(want.Fields), "extra"), Active: true}
	if !want.CoveredBy([]AppSubscription{{Object: "page", CallbackURL: testCallback, Fields: []string{"leadgen"}, Active: true}, full}) {
		t.Fatal("a complete subscription was redone")
	}
	missing := full
	missing.Fields = want.Fields[1:]
	elsewhere := full
	elsewhere.CallbackURL = "https://homolog.example/webhooks/meta-ads"
	inactive := full
	inactive.Active = false
	for name, current := range map[string]AppSubscription{"missing field": missing, "other callback": elsewhere, "inactive": inactive} {
		if want.CoveredBy([]AppSubscription{current}) {
			t.Fatalf("%s counted as subscribed", name)
		}
	}
	if want.CoveredBy(nil) {
		t.Fatal("no subscription counted as subscribed")
	}
}
