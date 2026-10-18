package workspace

import "testing"

func TestEveryGroupScreenIsOpenedByGroupsView(t *testing.T) {
	screens := map[Screen][]string{
		ScreenUnofficialGroups:              nil,
		ScreenUnofficialGroupsList:          nil,
		ScreenUnofficialGroupDetail:         {"groupId"},
		ScreenUnofficialGroupPeople:         nil,
		ScreenUnofficialGroupAnalyses:       nil,
		ScreenUnofficialGroupAnalysisDetail: {"analysisId"},
	}
	for screen, params := range screens {
		use, ok := CapabilityForScreen(screen)
		if !ok || use.Capability.Key != "unofficial_groups.view" {
			t.Errorf("%s must be opened by unofficial_groups.view, got %q", screen, use.Capability.Key)
		}
		if got := screen.Params(); len(got) != len(params) || (len(params) > 0 && got[0] != params[0]) {
			t.Errorf("%s params = %v, want %v", screen, got, params)
		}
	}
}

func TestTheGroupCapabilities(t *testing.T) {
	read := PermissionEntry{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead}
	cases := []struct {
		key      CapabilityKey
		requires []PermissionEntry
	}{
		{"unofficial_groups.view", []PermissionEntry{read}},
		{"unofficial_groups.join", []PermissionEntry{read, {Resource: ResourceUnofficialWhatsAppGroups, Action: ActionCreate}}},
		{"unofficial_groups.edit", []PermissionEntry{read, {Resource: ResourceUnofficialWhatsAppGroups, Action: ActionUpdate}}},
		{"unofficial_groups.analyses", []PermissionEntry{read, {Resource: ResourceUnofficialWhatsAppGroups, Action: ActionConfigure}}},
		{"unofficial_groups.delete", []PermissionEntry{read, {Resource: ResourceUnofficialWhatsAppGroups, Action: ActionDelete}}},
		{"unofficial_groups.export", []PermissionEntry{read, {Resource: ResourceUnofficialWhatsAppGroups, Action: ActionExport}}},
		{"unofficial_groups.to_leads", []PermissionEntry{read, {Resource: ResourceLeads, Action: ActionRead}, {Resource: ResourceLeads, Action: ActionCreate}}},
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

func TestGroupActionsNeedReadingAndCarryTheirRisks(t *testing.T) {
	read := PermissionEntry{Resource: ResourceUnofficialWhatsAppGroups, Action: ActionRead}
	risks := map[Action]RiskKind{
		ActionConfigure: RiskSpendsBalance,
		ActionDelete:    RiskDeletesData,
		ActionExport:    RiskSensitiveData,
	}
	seen := map[Action]bool{}
	for _, def := range ResourceActions[ResourceUnofficialWhatsAppGroups] {
		seen[def.ActionName] = true
		if def.ActionName != ActionRead && !containsPermission(def.Requires, read) {
			t.Errorf("unofficial_whatsapp_groups:%s must require reading the groups", def.ActionName)
		}
		if want, ok := risks[def.ActionName]; ok && !containsRisk(def.Risks, want) {
			t.Errorf("unofficial_whatsapp_groups:%s must be flagged %s", def.ActionName, want)
		}
	}
	for _, action := range []Action{ActionRead, ActionCreate, ActionUpdate, ActionDelete, ActionExport, ActionConfigure} {
		if !seen[action] {
			t.Errorf("unofficial_whatsapp_groups:%s is not an action of the resource", action)
		}
	}
}

func TestMarketingRunsGroupsAndTheAnalystOnlyReadsThem(t *testing.T) {
	cases := []struct {
		preset RolePresetKey
		has    []CapabilityKey
		lacks  []CapabilityKey
	}{
		{PresetMarketing, []CapabilityKey{"unofficial_groups.view", "unofficial_groups.join", "unofficial_groups.edit", "unofficial_groups.analyses", "unofficial_groups.export", "unofficial_groups.to_leads"}, []CapabilityKey{"unofficial_groups.delete"}},
		{PresetAnalyst, []CapabilityKey{"unofficial_groups.view"}, []CapabilityKey{"unofficial_groups.join", "unofficial_groups.edit", "unofficial_groups.analyses", "unofficial_groups.delete", "unofficial_groups.export"}},
	}
	for _, tc := range cases {
		preset, _ := RolePresetByKey(tc.preset)
		got := map[CapabilityKey]bool{}
		for _, c := range preset.Capabilities {
			got[c] = true
		}
		for _, c := range tc.has {
			if !got[c] {
				t.Errorf("preset %q lacks %s", tc.preset, c)
			}
		}
		for _, c := range tc.lacks {
			if got[c] {
				t.Errorf("preset %q must not grant %s", tc.preset, c)
			}
		}
	}
}
