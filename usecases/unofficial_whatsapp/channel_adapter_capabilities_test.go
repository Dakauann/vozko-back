package unofficial_whatsapp

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

func TestAdapterDescribesTheUnofficialWhatsAppCapabilities(t *testing.T) {
	adapter, _, _, _ := adapterFixture(t, nil)
	caps, err := conversation.CapabilitiesOf(conversation.NewAdapterRegistry(adapter), shared.EntryTypeUnofficialWhatsApp)
	if err != nil {
		t.Fatalf("CapabilitiesOf: %v", err)
	}
	if caps.MaxTextRunes != uw.MaxTextRunes {
		t.Errorf("caps = %+v, want the unofficial WhatsApp text limit", caps)
	}
}
