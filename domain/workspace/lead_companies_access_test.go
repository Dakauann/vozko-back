package workspace

import "testing"

func TestTheLeadCompaniesScreenIsOpenedByLeadCompaniesView(t *testing.T) {
	use, ok := CapabilityForScreen(ScreenLeadCompanies)
	if !ok || use.Capability.Key != "lead_companies.view" {
		t.Fatalf("lead_companies must be opened by lead_companies.view, got %q", use.Capability.Key)
	}
	if params := ScreenLeadCompanies.Params(); len(params) != 0 {
		t.Fatalf("lead_companies params = %v, want none", params)
	}
}

func TestTheLeadCompanyCapabilities(t *testing.T) {
	read := PermissionEntry{Resource: ResourceLeadCompanies, Action: ActionRead}
	cases := []struct {
		key      CapabilityKey
		requires []PermissionEntry
	}{
		{"lead_companies.view", []PermissionEntry{read}},
		{"lead_companies.create", []PermissionEntry{read, {Resource: ResourceLeadCompanies, Action: ActionCreate}}},
		{"lead_companies.edit", []PermissionEntry{read, {Resource: ResourceLeadCompanies, Action: ActionUpdate}}},
		{"lead_companies.delete", []PermissionEntry{read, {Resource: ResourceLeadCompanies, Action: ActionDelete}}},
		{"lead_companies.membership", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionRead}, {Resource: ResourceLeads, Action: ActionUpdate}}},
	}
	for _, tc := range cases {
		requires, ok := CapabilityRequires(tc.key)
		if !ok {
			t.Fatalf("%s is missing from the catalog", tc.key)
		}
		if len(requires) != len(tc.requires) {
			t.Fatalf("%s requires %v, want %v", tc.key, requires, tc.requires)
		}
		for i, p := range tc.requires {
			if requires[i] != p {
				t.Errorf("%s requires %v, want %v", tc.key, requires, tc.requires)
			}
		}
	}
}

func TestChangingLeadCompaniesNeedsReadingThemAndDeletingIsFlagged(t *testing.T) {
	read := PermissionEntry{Resource: ResourceLeadCompanies, Action: ActionRead}
	seen := map[Action]bool{}
	for _, def := range ResourceActions[ResourceLeadCompanies] {
		seen[def.ActionName] = true
		if def.ActionName != ActionRead && !containsPermission(def.Requires, read) {
			t.Errorf("lead_companies:%s must require lead_companies:read", def.ActionName)
		}
		if def.ActionName == ActionDelete && !containsRisk(def.Risks, RiskDeletesData) {
			t.Error("lead_companies:delete must be flagged as deleting data")
		}
	}
	for _, action := range []Action{ActionRead, ActionCreate, ActionUpdate, ActionDelete} {
		if !seen[action] {
			t.Errorf("lead_companies:%s is not an action of the resource", action)
		}
	}
}

func TestEveryLeadReaderSeesCompaniesAndOnlyManagersChangeThem(t *testing.T) {
	changes := []CapabilityKey{"lead_companies.create", "lead_companies.edit", "lead_companies.delete", "lead_companies.membership"}
	cases := []struct {
		preset RolePresetKey
		has    []CapabilityKey
		lacks  []CapabilityKey
	}{
		{PresetOperator, []CapabilityKey{"lead_companies.view"}, changes},
		{PresetSupervisor, []CapabilityKey{"lead_companies.view"}, changes},
		{PresetSales, []CapabilityKey{"lead_companies.view"}, changes},
		{PresetAnalyst, []CapabilityKey{"lead_companies.view"}, changes},
		{PresetMarketing, []CapabilityKey{"lead_companies.view"}, changes},
		{PresetManager, append([]CapabilityKey{"lead_companies.view"}, changes...), nil},
	}
	for _, tc := range cases {
		preset, ok := RolePresetByKey(tc.preset)
		if !ok {
			t.Fatalf("preset %s is missing", tc.preset)
		}
		held := map[CapabilityKey]bool{}
		for _, key := range preset.Capabilities {
			held[key] = true
		}
		for _, key := range tc.has {
			if !held[key] {
				t.Errorf("%s must hold %s", tc.preset, key)
			}
		}
		for _, key := range tc.lacks {
			if held[key] {
				t.Errorf("%s must not hold %s", tc.preset, key)
			}
		}
	}
}
