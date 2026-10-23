package copilot

import (
	"testing"

	"vozko/domain/readiness"
)

func TestActionCardCarriesTheLiveCapability(t *testing.T) {
	reais := 4.5
	snap := &readiness.Snapshot{SubscriptionActive: true, BalanceMicros: 900, BalanceBRL: &reais, Capabilities: []readiness.Status{
		{Capability: readiness.OfficialWhatsApp, Usage: &readiness.Usage{Used: 1, Total: 1}, Blocker: readiness.BlockerAtLimit},
	}}
	card, ok := NewActionCard(ActionConnectWhatsAppBusiness, snap)
	if !ok || card.Status == nil || card.Status.Blocker != readiness.BlockerAtLimit || card.Status.CanAdd {
		t.Fatalf("card = %+v", card)
	}
	if _, ok := NewActionCard(ActionConnectTelegram, snap); ok {
		t.Fatal("a card was built for a capability the snapshot does not know")
	}
	if card, ok := NewActionCard(ActionTopUpBalance, snap); !ok || card.BalanceMicros != 900 || card.BalanceBRL == nil || *card.BalanceBRL != 4.5 || card.Status != nil {
		t.Fatalf("balance card = %+v", card)
	}
}

func TestActionKindsAreAClosedCatalog(t *testing.T) {
	if ActionKind("open_url").Valid() || !ActionConnectInstagram.Valid() {
		t.Fatal("the catalog must be closed")
	}
}

func TestConnectFacebookOffersTheFacebookCapability(t *testing.T) {
	if c, ok := ActionConnectFacebook.Capability(); !ok || c != readiness.Facebook {
		t.Fatalf("capability = %q, %v", c, ok)
	}
}

func TestConnectMercadoLivreOffersTheMercadoLivreCapability(t *testing.T) {
	if ActionConnectMercadoLivre != "connect_mercadolivre" || !ActionConnectMercadoLivre.Valid() {
		t.Fatalf("action = %q", ActionConnectMercadoLivre)
	}
	if c, ok := ActionConnectMercadoLivre.Capability(); !ok || c != readiness.MercadoLivre || string(c) != "mercadolivre" {
		t.Fatalf("capability = %q, %v", c, ok)
	}
}

func TestConnectEmailOffersTheEmailCapability(t *testing.T) {
	if ActionConnectEmail != "connect_email" || !ActionConnectEmail.Valid() {
		t.Fatalf("action = %q", ActionConnectEmail)
	}
	if c, ok := ActionConnectEmail.Capability(); !ok || c != readiness.Email || string(c) != "email" {
		t.Fatalf("capability = %q, %v", c, ok)
	}
}
