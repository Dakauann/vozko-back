package copilot

import (
	"testing"

	"vozko/domain/readiness"
)

func TestActionCardCarriesTheLiveCapability(t *testing.T) {
	snap := &readiness.Snapshot{SubscriptionActive: true, BalanceMicros: 900, Capabilities: []readiness.Status{
		{Capability: readiness.OfficialWhatsApp, Usage: &readiness.Usage{Used: 1, Total: 1}, Blocker: readiness.BlockerAtLimit},
	}}
	card, ok := NewActionCard(ActionConnectWhatsAppBusiness, snap)
	if !ok || card.Status == nil || card.Status.Blocker != readiness.BlockerAtLimit || card.Status.CanAdd {
		t.Fatalf("card = %+v", card)
	}
	if _, ok := NewActionCard(ActionConnectTelegram, snap); ok {
		t.Fatal("a card was built for a capability the snapshot does not know")
	}
	if card, ok := NewActionCard(ActionTopUpBalance, snap); !ok || card.BalanceMicros != 900 || card.Status != nil {
		t.Fatalf("balance card = %+v", card)
	}
}

func TestActionKindsAreAClosedCatalog(t *testing.T) {
	if ActionKind("open_url").Valid() || !ActionConnectInstagram.Valid() {
		t.Fatal("the catalog must be closed")
	}
}
