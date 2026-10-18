package workspace

import (
	"slices"
	"testing"
)

func TestLeadsConsentIsAnActionOfLeadsThatNeedsReadAndUpdate(t *testing.T) {
	for _, def := range ResourceActions[ResourceLeads] {
		if def.ActionName != ActionConsent {
			continue
		}
		for _, want := range []PermissionEntry{need(ResourceLeads, ActionRead), need(ResourceLeads, ActionUpdate)} {
			if !slices.Contains(def.Requires, want) {
				t.Fatalf("leads:consent lacks the prerequisite %s", want.Key())
			}
		}
		return
	}
	t.Fatal("leads:consent must be a lead action")
}

func TestTheLeadsConsentCapabilityRecordsGrantsOnTopOfEditing(t *testing.T) {
	c, ok := CapabilityByKey("leads.consent")
	if !ok {
		t.Fatal("leads.consent is missing from the catalog")
	}
	want := []PermissionEntry{need(ResourceLeads, ActionRead), need(ResourceLeads, ActionUpdate), need(ResourceLeads, ActionConsent)}
	if len(c.Requires) != len(want) {
		t.Fatalf("leads.consent requires %v", c.Requires)
	}
	for _, p := range want {
		if !slices.Contains(c.Requires, p) {
			t.Fatalf("leads.consent lacks %s", p.Key())
		}
	}
	if c.ManagersOnly {
		t.Fatal("recording consent is not reserved to managers")
	}
}

func TestEveryPresetBuiltOnTheOperatorRecordsConsent(t *testing.T) {
	for _, preset := range RolePresets {
		has := slices.Contains(preset.Capabilities, "leads.consent")
		want := slices.Contains(preset.Capabilities, "inbox.contact_edit")
		if has != want {
			t.Errorf("preset %q records consent = %v, want %v", preset.Key, has, want)
		}
	}
}
