package conversation

import (
	"testing"

	"vozko/domain/shared"
)

func TestEveryMessagingEntryTypeHasAValidMessageChannel(t *testing.T) {
	for _, e := range shared.KnownEntryTypes() {
		if !e.Valid() {
			continue
		}
		if !MessageChannel(e).Valid() {
			t.Errorf("%q is a messaging entry type but its messages would be refused on write", e)
		}
	}
}

func TestMercadoLivreMessageChannel(t *testing.T) {
	if MessageChannelMercadoLivre != "mercadolivre" || !MessageChannelMercadoLivre.Valid() {
		t.Fatalf("mercadolivre message channel = %q valid=%v", MessageChannelMercadoLivre, MessageChannelMercadoLivre.Valid())
	}
	for _, bad := range []MessageChannel{"", "mercado_livre", "MercadoLivre", "mercadopago"} {
		if bad.Valid() {
			t.Errorf("%q must not be a valid message channel", bad)
		}
	}
}
