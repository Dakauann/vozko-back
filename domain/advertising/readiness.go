package advertising

import "net/url"

type ReadinessKey string

const (
	ReadyConnection     ReadinessKey = "connection"
	ReadyRole           ReadinessKey = "advertiser_role"
	ReadyAccountStatus  ReadinessKey = "account_status"
	ReadyAccountDetails ReadinessKey = "account_details"
	ReadyPaymentMethod  ReadinessKey = "payment_method"
	ReadyPage           ReadinessKey = "page"
	ReadyPhone          ReadinessKey = "phone_verification"
	ReadyEmail          ReadinessKey = "email_verification"
	ReadyAudienceTerms  ReadinessKey = "custom_audience_terms"
	ReadyPixel          ReadinessKey = "pixel"
)

type ReadinessState string

const (
	StateReady   ReadinessState = "ready"
	StateMissing ReadinessState = "missing"
	StateUnknown ReadinessState = "unknown"
)

type InAppAction string

const (
	ActionReconnect   InAppAction = "reconnect"
	ActionSync        InAppAction = "sync"
	ActionCreatePixel InAppAction = "create_pixel"
)

type ReadinessAction struct {
	InApp  InAppAction
	Portal string
}

type ReadinessItem struct {
	Key      ReadinessKey
	State    ReadinessState
	Required bool
	Action   ReadinessAction
}

type AccountReadiness struct {
	Items []ReadinessItem
}

func (r AccountReadiness) Blocking() []ReadinessKey {
	var keys []ReadinessKey
	for _, item := range r.Items {
		if item.Required && item.State != StateReady {
			keys = append(keys, item.Key)
		}
	}
	return keys
}

func (r AccountReadiness) CanPublish() bool { return len(r.Blocking()) == 0 }

type Observation struct {
	read bool
	ok   bool
}

var Unobserved = Observation{}

func Observed(ok bool) Observation { return Observation{read: true, ok: ok} }

func (o Observation) state() ReadinessState {
	switch {
	case !o.read:
		return StateUnknown
	case o.ok:
		return StateReady
	}
	return StateMissing
}

type ReadinessFacts struct {
	Page          Observation
	AudienceTerms Observation
	Pixel         Observation
}

func BuildReadiness(a *AdAccount, facts ReadinessFacts) AccountReadiness {
	items := make([]ReadinessItem, 0, len(spendChecks)+5)
	connected := true
	for _, c := range spendChecks {
		state := StateUnknown
		if connected {
			state = Observed(c.check(a) == nil).state()
		}
		if c.item == ReadyConnection {
			connected = state == StateReady
		}
		items = append(items, item(c.item, state, true, accountAction(a, c.item)))
	}
	if !connected {
		facts = ReadinessFacts{}
	}
	items = append(items,
		item(ReadyPage, facts.Page.state(), true, ReadinessAction{Portal: PageCreatePortalURL}),
		checkedAtMeta(ReadyPhone, PhoneVerificationPortalURL),
		checkedAtMeta(ReadyEmail, EmailVerificationPortalURL),
		item(ReadyAudienceTerms, facts.AudienceTerms.state(), false, ReadinessAction{Portal: CustomAudienceTermsURL(a.MetaAccountID)}),
		item(ReadyPixel, facts.Pixel.state(), false, ReadinessAction{InApp: ActionCreatePixel}),
	)
	return AccountReadiness{Items: items}
}

func item(key ReadinessKey, state ReadinessState, required bool, fix ReadinessAction) ReadinessItem {
	out := ReadinessItem{Key: key, State: state, Required: required}
	if state == StateMissing {
		out.Action = fix
	}
	return out
}

func checkedAtMeta(key ReadinessKey, portal string) ReadinessItem {
	return ReadinessItem{Key: key, State: StateUnknown, Action: ReadinessAction{Portal: portal}}
}

func accountAction(a *AdAccount, key ReadinessKey) ReadinessAction {
	switch key {
	case ReadyConnection:
		return ReadinessAction{InApp: ActionReconnect}
	case ReadyRole:
		return ReadinessAction{Portal: BusinessSettingsPortalURL}
	case ReadyAccountStatus:
		return ReadinessAction{Portal: statusPortalURL(a)}
	case ReadyAccountDetails:
		return ReadinessAction{InApp: ActionSync}
	case ReadyPaymentMethod:
		return ReadinessAction{Portal: BillingPortalURL}
	}
	return ReadinessAction{}
}

func (s MetaAccountStatus) owesPayment() bool {
	return s == MetaAccountUnsettled || s == MetaAccountPendingSettlement || s == MetaAccountInGracePeriod
}

func statusPortalURL(a *AdAccount) string {
	if a.MetaStatus.owesPayment() {
		return BillingPortalURL
	}
	return AccountReviewPortalURL
}

const (
	BillingPortalURL           = "https://business.facebook.com/latest/billing_hub"
	BusinessSettingsPortalURL  = "https://business.facebook.com/latest/settings"
	PageCreatePortalURL        = "https://www.facebook.com/pages/creation/"
	AccountReviewPortalURL     = "https://www.facebook.com/business/help/530209463124901"
	PhoneVerificationPortalURL = "https://business.facebook.com/latest/settings/authorizations_verifications"
	EmailVerificationPortalURL = "https://www.facebook.com/help/162801153783275"
	customAudienceTermsURL     = "https://business.facebook.com/ads/manage/customaudiences/tos/"
)

func CustomAudienceTermsURL(metaAccountID string) string {
	return customAudienceTermsURL + "?" + url.Values{"act": {NormalizeAccountID(metaAccountID)}}.Encode()
}
