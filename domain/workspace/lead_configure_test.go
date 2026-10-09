package workspace

import "testing"

func TestLeadsConfigureIsAnActionOfLeadsThatRequiresRead(t *testing.T) {
	for _, def := range ResourceActions[ResourceLeads] {
		if def.ActionName != ActionConfigure {
			continue
		}
		for _, req := range def.Requires {
			if req == (PermissionEntry{Resource: ResourceLeads, Action: ActionRead}) {
				return
			}
		}
		t.Fatalf("leads:configure must require leads:read, got %v", def.Requires)
	}
	t.Fatal("leads:configure must be a lead action")
}

func TestTheLeadsConfigureCapabilityCarriesItsPrerequisites(t *testing.T) {
	c, ok := CapabilityByKey("leads.configure")
	if !ok {
		t.Fatal("leads.configure is missing from the catalog")
	}
	want := map[PermissionEntry]bool{
		{Resource: ResourceLeads, Action: ActionRead}:      true,
		{Resource: ResourceLeads, Action: ActionConfigure}: true,
	}
	if len(c.Requires) != len(want) {
		t.Fatalf("leads.configure requires %v", c.Requires)
	}
	for _, p := range c.Requires {
		if !want[p] {
			t.Fatalf("leads.configure requires %s, which it should not", p.Key())
		}
	}
}

func TestOnlyTheManagerPresetConfiguresLeadFields(t *testing.T) {
	for _, preset := range RolePresets {
		has := false
		for _, key := range preset.Capabilities {
			has = has || key == "leads.configure"
		}
		if has != (preset.Key == PresetManager) {
			t.Errorf("preset %q configures lead fields = %v", preset.Key, has)
		}
	}
}
