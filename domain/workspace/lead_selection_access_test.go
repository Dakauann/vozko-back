package workspace

import "testing"

func TestLeadsGainExportAndBulkUpdate(t *testing.T) {
	read := PermissionEntry{Resource: ResourceLeads, Action: ActionRead}
	update := PermissionEntry{Resource: ResourceLeads, Action: ActionUpdate}
	want := map[Action][]PermissionEntry{
		ActionExport:     {read},
		ActionBulkUpdate: {read, update},
	}
	actions := map[Action]ActionDefinition{}
	for _, def := range ResourceActions[ResourceLeads] {
		actions[def.ActionName] = def
	}
	for action, requires := range want {
		def, ok := actions[action]
		if !ok {
			t.Fatalf("leads:%s must be a lead action", action)
		}
		if len(def.Requires) != len(requires) {
			t.Fatalf("leads:%s requires %v, want %v", action, def.Requires, requires)
		}
		for i, p := range requires {
			if def.Requires[i] != p {
				t.Errorf("leads:%s requires %v, want %v", action, def.Requires, requires)
			}
		}
	}
	risky := false
	for _, risk := range RisksOf(PermissionEntry{Resource: ResourceLeads, Action: ActionExport}) {
		risky = risky || risk.Kind == RiskSensitiveData
	}
	if !risky {
		t.Error("leads:export must be flagged as sensitive data")
	}
}

func TestTheLeadSelectionCapabilities(t *testing.T) {
	read := PermissionEntry{Resource: ResourceLeads, Action: ActionRead}
	cases := []struct {
		key      CapabilityKey
		requires []PermissionEntry
	}{
		{"leads.bulk_edit", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionUpdate}, {Resource: ResourceLeads, Action: ActionBulkUpdate}}},
		{"leads.export", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionExport}, {Resource: ResourceReports, Action: ActionCreate}, {Resource: ResourceReports, Action: ActionRead}}},
		{"leads.meta_audience", []PermissionEntry{read, {Resource: ResourceAds, Action: ActionCreate}}},
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
		c, _ := CapabilityByKey(tc.key)
		if c.ManagersOnly {
			t.Errorf("%s must not be managers only", tc.key)
		}
	}
}

func TestCapabilityRequiresRefusesAnUnknownKey(t *testing.T) {
	if requires, ok := CapabilityRequires("leads.nothing"); ok || requires != nil {
		t.Fatalf("unknown capability answered %v, %v", requires, ok)
	}
}

func TestOnlyTheManagerPresetActsOnLeadSelections(t *testing.T) {
	for _, key := range []CapabilityKey{"leads.bulk_edit", "leads.export", "leads.meta_audience"} {
		for _, preset := range RolePresets {
			has := false
			for _, held := range preset.Capabilities {
				has = has || held == key
			}
			if has != (preset.Key == PresetManager) {
				t.Errorf("preset %q holds %s = %v", preset.Key, key, has)
			}
		}
	}
}
