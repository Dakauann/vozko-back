package advertising

import "net/url"

type PortalScreen string

const (
	PortalBilling           PortalScreen = "billing"
	PortalAccountRoles      PortalScreen = "account_roles"
	PortalPhoneVerification PortalScreen = "phone_verification"
)

type portalTarget struct {
	base  string
	query func(businessID, accountID string) url.Values
}

var businessPortals = map[PortalScreen]portalTarget{
	PortalBilling: {"https://business.facebook.com/latest/billing_hub/payment_settings", func(b, a string) url.Values {
		return url.Values{"business_id": {b}, "asset_id": {a}}
	}},
	PortalAccountRoles: {"https://business.facebook.com/latest/settings/ad_accounts", func(b, a string) url.Values {
		return url.Values{"business_id": {b}, "selected_asset_id": {a}}
	}},
	PortalPhoneVerification: {"https://business.facebook.com/latest/settings/authorizations_verifications", func(b, _ string) url.Values {
		return url.Values{"business_id": {b}}
	}},
}

var personalPortals = map[PortalScreen]portalTarget{
	PortalBilling: {"https://business.facebook.com/billing_hub/payment_settings", func(_, a string) url.Values {
		return url.Values{"asset_id": {a}}
	}},
	PortalAccountRoles: {"https://adsmanager.facebook.com/adsmanager/manage/accounts", func(_, a string) url.Values {
		return url.Values{"act": {a}}
	}},
}

func (a *AdAccount) PortalURL(screen PortalScreen) string {
	if a == nil {
		return ""
	}
	accountID := NormalizeAccountID(a.MetaAccountID)
	if accountID == "" {
		return ""
	}
	portals := personalPortals
	if a.BusinessID != "" {
		portals = businessPortals
	}
	target, ok := portals[screen]
	if !ok {
		return ""
	}
	return target.base + "?" + target.query(a.BusinessID, accountID).Encode()
}
