package advertising

import (
	"net/url"
	"testing"
)

func portalQuery(t *testing.T, raw, wantPath string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host+u.Path != wantPath {
		t.Fatalf("url %s, want https://%s", raw, wantPath)
	}
	return u.Query()
}

func businessAccount() *AdAccount {
	a := spendableAccount()
	a.MetaAccountID = "act_163167293135040"
	a.BusinessID = "777"
	return a
}

func personalAccount() *AdAccount {
	a := businessAccount()
	a.BusinessID = ""
	return a
}

func TestBusinessBillingOpensTheOwningPortfolioOnThatAccount(t *testing.T) {
	q := portalQuery(t, businessAccount().PortalURL(PortalBilling), "business.facebook.com/latest/billing_hub/payment_settings")
	if q.Get("business_id") != "777" || q.Get("asset_id") != "163167293135040" {
		t.Fatalf("query %v", q)
	}
}

func TestBusinessRolesOpenTheAccountInTheOwningPortfolio(t *testing.T) {
	q := portalQuery(t, businessAccount().PortalURL(PortalAccountRoles), "business.facebook.com/latest/settings/ad_accounts")
	if q.Get("business_id") != "777" || q.Get("selected_asset_id") != "163167293135040" {
		t.Fatalf("query %v", q)
	}
}

func TestBusinessPhoneVerificationOpensTheOwningPortfolio(t *testing.T) {
	q := portalQuery(t, businessAccount().PortalURL(PortalPhoneVerification), "business.facebook.com/latest/settings/authorizations_verifications")
	if q.Get("business_id") != "777" {
		t.Fatalf("query %v", q)
	}
}

func TestPersonalBillingOpensThatAccount(t *testing.T) {
	q := portalQuery(t, personalAccount().PortalURL(PortalBilling), "business.facebook.com/billing_hub/payment_settings")
	if q.Get("asset_id") != "163167293135040" || q.Has("business_id") {
		t.Fatalf("query %v", q)
	}
}

func TestPersonalRolesOpenThatAccountInAdsManager(t *testing.T) {
	q := portalQuery(t, personalAccount().PortalURL(PortalAccountRoles), "adsmanager.facebook.com/adsmanager/manage/accounts")
	if q.Get("act") != "163167293135040" {
		t.Fatalf("query %v", q)
	}
}

func TestNoPortalLinkWhenItCannotBeScoped(t *testing.T) {
	if got := personalAccount().PortalURL(PortalPhoneVerification); got != "" {
		t.Fatalf("personal phone verification %s", got)
	}
	noID := businessAccount()
	noID.MetaAccountID = ""
	if got := noID.PortalURL(PortalBilling); got != "" {
		t.Fatalf("account without id %s", got)
	}
	var none *AdAccount
	if got := none.PortalURL(PortalBilling); got != "" {
		t.Fatalf("nil account %s", got)
	}
	if got := businessAccount().PortalURL(PortalScreen("other")); got != "" {
		t.Fatalf("unknown screen %s", got)
	}
}
