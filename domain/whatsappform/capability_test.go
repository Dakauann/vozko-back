package whatsappform

import (
	"testing"

	"vozko/domain/workspace"
)

func TestEveryFormCapabilityIsInTheAccessCatalog(t *testing.T) {
	keys := []workspace.CapabilityKey{
		CapabilityView, CapabilityBuild, CapabilityPublish, CapabilitySend, CapabilityDelete,
		CapabilityResponses, CapabilityResponsesSensitive, CapabilityExport, CapabilityResponsesDelete,
	}
	for _, key := range keys {
		entries, err := workspace.CapabilitiesRequire([]workspace.CapabilityKey{key})
		if err != nil || len(entries) == 0 {
			t.Errorf("%s must resolve to permissions, got %v %v", key, entries, err)
		}
	}
}

func TestFormScreensAreRegistered(t *testing.T) {
	for _, s := range []workspace.Screen{workspace.ScreenWhatsAppForms, workspace.ScreenWhatsAppFormNew, workspace.ScreenWhatsAppFormDetail, workspace.ScreenWhatsAppFormBuilder} {
		if s == "" {
			t.Fatal("every form screen has a key")
		}
	}
}
