package advertisinghttp

import (
	"slices"
	"testing"

	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

func TestReadinessResponseListsBlockersAndHowToFixThem(t *testing.T) {
	account := presentableAccount("ADVERTISE")
	account.HasFunding = false
	checklist := advertising.BuildReadiness(account, advertising.ReadinessFacts{Page: advertising.Observed(true), Pixel: advertising.Observed(false)})
	got := presentReadiness(&adsuc.Readiness{Account: account, Checklist: checklist, Billing: &advertising.RemoteBilling{Balance: 900}})

	if got.Ready || !slices.Equal(got.Blocking, []string{"payment_method"}) {
		t.Fatalf("ready %v blocking %v", got.Ready, got.Blocking)
	}
	actions := map[string]*ReadinessActionResponse{}
	for _, item := range got.Items {
		actions[item.Key] = item.Action
	}
	if a := actions["payment_method"]; a == nil || a.Kind != "portal" || a.URL != advertising.BillingPortalURL {
		t.Fatalf("payment action %+v", a)
	}
	if a := actions["pixel"]; a == nil || a.Kind != "in_app" || a.Key != "create_pixel" {
		t.Fatalf("pixel action %+v", a)
	}
	if actions["page"] != nil {
		t.Fatal("a ready item offered an action")
	}
	if got.Billing == nil || got.Billing.Balance != 900 || got.Billing.PortalURL != advertising.BillingPortalURL || got.Billing.PaymentMethod != "" {
		t.Fatalf("billing %+v", got.Billing)
	}
}

func TestReadinessResponseNeverOmitsTheBlockingList(t *testing.T) {
	account := presentableAccount("MANAGE")
	got := presentReadiness(&adsuc.Readiness{Account: account, Checklist: advertising.BuildReadiness(account, advertising.ReadinessFacts{Page: advertising.Observed(true)})})
	if !got.Ready || got.Blocking == nil || got.Billing != nil {
		t.Fatalf("%+v", got)
	}
}
