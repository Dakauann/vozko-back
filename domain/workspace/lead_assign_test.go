package workspace

import (
	"strings"
	"testing"
)

func TestLeadsAssignIsAnActionOfLeads(t *testing.T) {
	for _, def := range ResourceActions[ResourceLeads] {
		if def.ActionName == ActionAssign {
			return
		}
	}
	t.Fatal("leads:assign must be a lead action")
}

func TestTheLeadsAssignCapabilityCarriesItsPrerequisites(t *testing.T) {
	c, ok := CapabilityByKey("leads.assign")
	if !ok {
		t.Fatal("leads.assign is missing from the catalog")
	}
	want := map[PermissionEntry]bool{
		{Resource: ResourceLeads, Action: ActionRead}:   true,
		{Resource: ResourceLeads, Action: ActionAssign}: true,
		{Resource: ResourceMembers, Action: ActionRead}: true,
	}
	if len(c.Requires) != len(want) {
		t.Fatalf("leads.assign requires %v", c.Requires)
	}
	for _, p := range c.Requires {
		if !want[p] {
			t.Fatalf("leads.assign requires %s, which it should not", p.Key())
		}
	}
}

func TestOnlyTheManagerPresetAssignsLeadOwners(t *testing.T) {
	for _, preset := range RolePresets {
		has := false
		for _, key := range preset.Capabilities {
			has = has || key == "leads.assign"
		}
		if has != (preset.Key == PresetManager) {
			t.Errorf("preset %q assigns lead owners = %v", preset.Key, has)
		}
	}
}

func TestTheLeadsViewCapabilityNoLongerPromisesConversationHistory(t *testing.T) {
	c, ok := CapabilityByKey("leads.view")
	if !ok {
		t.Fatal("leads.view is missing")
	}
	if strings.Contains(c.Description, "histórico de campanhas e conversas") {
		t.Fatalf("leads.view still promises every conversation: %q", c.Description)
	}
}
