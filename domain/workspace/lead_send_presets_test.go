package workspace

import (
	"slices"
	"testing"
)

func TestSendsFromASelectionGoToManagersOnlyByPreset(t *testing.T) {
	sends := []CapabilityKey{"leads.send_template", "leads.send_unofficial"}
	for _, key := range sends {
		if _, ok := CapabilityByKey(key); !ok {
			t.Fatalf("capability %s is missing from the catalog", key)
		}
	}
	for _, preset := range RolePresets {
		for _, key := range sends {
			has := slices.Contains(preset.Capabilities, key)
			switch preset.Key {
			case PresetManager:
				if !has {
					t.Fatalf("the manager preset lacks %s", key)
				}
			default:
				if has {
					t.Fatalf("preset %s grants %s, only managers get it by default", preset.Key, key)
				}
			}
		}
	}
}

func TestTheTemplateSendCapabilityCarriesWhatTheDialogReads(t *testing.T) {
	requires, ok := CapabilityRequires("leads.send_template")
	if !ok {
		t.Fatal("leads.send_template is not in the catalog")
	}
	for _, want := range []PermissionEntry{
		need(ResourceLeads, ActionRead), need(ResourceWhatsAppCampaigns, ActionCreate), need(ResourceWhatsAppCampaigns, ActionStart),
		need(ResourceWhatsAppTemplates, ActionSend), need(ResourceWhatsAppTemplates, ActionRead), need(ResourceBusinessPhones, ActionRead), conversationsRead,
	} {
		if !slices.Contains(requires, want) {
			t.Fatalf("leads.send_template lacks %v", want)
		}
	}
}
