package copilot

import "vozko/domain/readiness"

type ActionKind string

const (
	ActionConnectWhatsAppBusiness   ActionKind = "connect_whatsapp_business"
	ActionConnectUnofficialWhatsApp ActionKind = "connect_unofficial_whatsapp"
	ActionConnectInstagram          ActionKind = "connect_instagram"
	ActionConnectTelegram           ActionKind = "connect_telegram"
	ActionConnectFacebook           ActionKind = "connect_facebook"
	ActionCreateWebchat             ActionKind = "create_webchat"
	ActionTopUpBalance              ActionKind = "top_up_balance"
	ActionManageSubscription        ActionKind = "manage_subscription"
	ActionOpenScreen                ActionKind = "open_screen"
	ActionPlaceCall                 ActionKind = "place_call"
	ActionAdReadiness               ActionKind = "ad_readiness"
	ActionConnectAdAccount          ActionKind = "connect_ad_account"
)

var actionCapabilities = map[ActionKind]readiness.Capability{
	ActionConnectWhatsAppBusiness:   readiness.OfficialWhatsApp,
	ActionConnectUnofficialWhatsApp: readiness.UnofficialWhatsApp,
	ActionConnectInstagram:          readiness.Instagram,
	ActionConnectTelegram:           readiness.Telegram,
	ActionConnectFacebook:           readiness.Facebook,
	ActionCreateWebchat:             readiness.Webchat,
}

func ActionKinds() []ActionKind {
	return []ActionKind{
		ActionConnectWhatsAppBusiness, ActionConnectUnofficialWhatsApp, ActionConnectInstagram,
		ActionConnectTelegram, ActionConnectFacebook, ActionCreateWebchat, ActionTopUpBalance, ActionManageSubscription,
	}
}

func (k ActionKind) Valid() bool {
	for _, known := range ActionKinds() {
		if k == known {
			return true
		}
	}
	return false
}

func (k ActionKind) Capability() (readiness.Capability, bool) {
	c, ok := actionCapabilities[k]
	return c, ok
}

type ActionCard struct {
	Kind               ActionKind        `json:"kind"`
	Status             *readiness.Status `json:"status,omitempty"`
	BalanceMicros      int64             `json:"balanceMicros"`
	SubscriptionActive bool              `json:"subscriptionActive"`
	Destination        *Destination      `json:"destination,omitempty"`
	Call               *CallIntent       `json:"call,omitempty"`
	AdAccountID        string            `json:"adAccountId,omitempty"`
	Question           *Question         `json:"question,omitempty"`
}

func NewConnectAdAccountCard() *ActionCard {
	return &ActionCard{Kind: ActionConnectAdAccount}
}

func NewAdReadinessCard(adAccountID string) *ActionCard {
	return &ActionCard{Kind: ActionAdReadiness, AdAccountID: adAccountID}
}

func NewActionCard(kind ActionKind, snap *readiness.Snapshot) (*ActionCard, bool) {
	card := &ActionCard{Kind: kind, BalanceMicros: snap.BalanceMicros, SubscriptionActive: snap.SubscriptionActive}
	capability, needsStatus := kind.Capability()
	if !needsStatus {
		return card, true
	}
	status, found := snap.Get(capability)
	if !found {
		return nil, false
	}
	card.Status = &status
	return card, true
}
