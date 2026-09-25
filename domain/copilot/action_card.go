package copilot

import "vozko/domain/readiness"

type ActionKind string

const (
	ActionConnectWhatsAppBusiness   ActionKind = "connect_whatsapp_business"
	ActionConnectUnofficialWhatsApp ActionKind = "connect_unofficial_whatsapp"
	ActionConnectInstagram          ActionKind = "connect_instagram"
	ActionConnectTelegram           ActionKind = "connect_telegram"
	ActionTopUpBalance              ActionKind = "top_up_balance"
	ActionManageSubscription        ActionKind = "manage_subscription"
)

var actionCapabilities = map[ActionKind]readiness.Capability{
	ActionConnectWhatsAppBusiness:   readiness.OfficialWhatsApp,
	ActionConnectUnofficialWhatsApp: readiness.UnofficialWhatsApp,
	ActionConnectInstagram:          readiness.Instagram,
	ActionConnectTelegram:           readiness.Telegram,
}

func ActionKinds() []ActionKind {
	return []ActionKind{
		ActionConnectWhatsAppBusiness, ActionConnectUnofficialWhatsApp, ActionConnectInstagram,
		ActionConnectTelegram, ActionTopUpBalance, ActionManageSubscription,
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
