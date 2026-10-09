package workspace

import "testing"

func TestLeadsGainSensitiveAndAddressReads(t *testing.T) {
	actions := map[Action]ActionDefinition{}
	for _, def := range ResourceActions[ResourceLeads] {
		actions[def.ActionName] = def
	}
	for _, action := range []Action{ActionReadSensitive, ActionReadAddresses} {
		def, ok := actions[action]
		if !ok {
			t.Fatalf("leads:%s must be a lead action", action)
		}
		if len(def.Requires) != 1 || def.Requires[0] != (PermissionEntry{Resource: ResourceLeads, Action: ActionRead}) {
			t.Errorf("leads:%s must require leads:read, got %v", action, def.Requires)
		}
		risky := false
		for _, risk := range RisksOf(PermissionEntry{Resource: ResourceLeads, Action: action}) {
			risky = risky || risk.Kind == RiskSensitiveData
		}
		if !risky {
			t.Errorf("leads:%s must be flagged as sensitive data", action)
		}
	}
}

func TestTheLeadCapabilitiesForSensitiveDataAddressesAndAnonymization(t *testing.T) {
	read := PermissionEntry{Resource: ResourceLeads, Action: ActionRead}
	cases := []struct {
		key          CapabilityKey
		requires     []PermissionEntry
		managersOnly bool
	}{
		{"leads.read_sensitive", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionReadSensitive}}, false},
		{"leads.read_addresses", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionReadAddresses}}, false},
		{"leads.anonymize", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionAnonymize}}, true},
	}
	for _, tc := range cases {
		c, ok := CapabilityByKey(tc.key)
		if !ok {
			t.Fatalf("%s is missing from the catalog", tc.key)
		}
		if c.ManagersOnly != tc.managersOnly {
			t.Errorf("%s managers only = %v", tc.key, c.ManagersOnly)
		}
		if len(c.Requires) != len(tc.requires) {
			t.Fatalf("%s requires %v", tc.key, c.Requires)
		}
		for i, p := range tc.requires {
			if c.Requires[i] != p {
				t.Errorf("%s requires %v, want %v", tc.key, c.Requires, tc.requires)
			}
		}
	}
}

func TestAnonymizingDeletesData(t *testing.T) {
	for _, risk := range RisksOf(PermissionEntry{Resource: ResourceLeads, Action: ActionAnonymize}) {
		if risk.Kind == RiskDeletesData {
			return
		}
	}
	t.Fatal("leads:anonymize must carry the deletes-data risk")
}

func TestAnonymizingIsANewPermissionNoLegacyRoleHolds(t *testing.T) {
	if ActionAnonymize == ActionDelete {
		t.Fatal("roles saved while leads:delete did nothing must not gain the erasure")
	}
	var anonymize *ActionDefinition
	for i, def := range ResourceActions[ResourceLeads] {
		if def.ActionName == ActionAnonymize {
			anonymize = &ResourceActions[ResourceLeads][i]
		}
	}
	if anonymize == nil {
		t.Fatal("leads:anonymize must be a lead action")
	}
	if len(anonymize.Requires) != 1 || anonymize.Requires[0] != (PermissionEntry{Resource: ResourceLeads, Action: ActionRead}) {
		t.Fatalf("leads:anonymize must require leads:read, got %v", anonymize.Requires)
	}
	for _, c := range CapabilitiesUsing(PermissionEntry{Resource: ResourceLeads, Action: ActionDelete}) {
		t.Errorf("leads:delete stays inert, but %s uses it", c.Capability.Key)
	}
}

func TestOnlyTheManagerPresetReadsSensitiveDataAndAddresses(t *testing.T) {
	for _, preset := range RolePresets {
		held := map[CapabilityKey]bool{}
		for _, key := range preset.Capabilities {
			held[key] = true
		}
		manager := preset.Key == PresetManager
		for _, key := range []CapabilityKey{"leads.read_sensitive", "leads.read_addresses"} {
			if held[key] != manager {
				t.Errorf("preset %q holds %s = %v", preset.Key, key, held[key])
			}
		}
		if held["leads.anonymize"] {
			t.Errorf("preset %q holds leads.anonymize, which only managers may use", preset.Key)
		}
	}
}

func TestCreatingALeadIsPartOfTheImportCapability(t *testing.T) {
	c, ok := CapabilityByKey("leads.import")
	if !ok || c.Description != "Cadastrar leads e importar planilhas" {
		t.Fatalf("leads.import = %+v", c)
	}
	if _, separate := CapabilityByKey("leads.create"); separate {
		t.Fatal("creating a lead is covered by leads.import, not a separate capability")
	}
}
