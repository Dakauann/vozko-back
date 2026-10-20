package webchat

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
)

func TestAdapterDescribesTheWebchatCapabilities(t *testing.T) {
	adapter, _, _, _, _ := adapterHarness()
	caps, err := conversation.CapabilitiesOf(conversation.NewAdapterRegistry(adapter), shared.EntryTypeWebchat)
	if err != nil {
		t.Fatalf("CapabilitiesOf: %v", err)
	}
	want := wcdomain.Descriptor().Capabilities
	if caps.MaxTextRunes != want.MaxTextRunes || caps.MaxTextBytes != want.MaxTextBytes {
		t.Errorf("caps = %+v, want the webchat text limits", caps)
	}
}
