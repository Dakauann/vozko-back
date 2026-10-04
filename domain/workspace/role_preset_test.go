package workspace

import (
	"errors"
	"testing"
)

func TestEveryPresetIsBuiltFromRealCapabilities(t *testing.T) {
	seen := map[RolePresetKey]bool{}
	for _, preset := range RolePresets {
		if seen[preset.Key] {
			t.Errorf("preset %q is declared twice", preset.Key)
		}
		seen[preset.Key] = true
		if preset.Name == "" || preset.Description == "" || len(preset.Highlights) == 0 {
			t.Errorf("preset %q needs a name, a description and highlights", preset.Key)
		}
		if _, err := preset.Permissions(); err != nil {
			t.Errorf("preset %q: %v", preset.Key, err)
		}
	}
}

func TestPresetsNeverGrantManagerOnlyCapabilities(t *testing.T) {
	for _, preset := range RolePresets {
		for _, key := range preset.Capabilities {
			if c, ok := CapabilityByKey(key); ok && c.ManagersOnly {
				t.Errorf("preset %q grants %q, which only managers may hold", preset.Key, key)
			}
		}
	}
}

func TestPresetPermissionsCarryTheirPrerequisites(t *testing.T) {
	for _, preset := range RolePresets {
		perms, _ := preset.Permissions()
		granted := map[PermissionEntry]bool{}
		for _, p := range perms {
			granted[p] = true
		}
		for _, p := range perms {
			for _, def := range ResourceActions[p.Resource] {
				if def.ActionName != p.Action {
					continue
				}
				for _, need := range def.Requires {
					if !granted[need] {
						t.Errorf("preset %q grants %s but not its prerequisite %s", preset.Key, p.Key(), need.Key())
					}
				}
			}
		}
	}
}

func TestFrontLinePresetsCannotManageAccessDeleteDataOrChangeBilling(t *testing.T) {
	forbidden := map[RiskKind]bool{RiskManagesAccess: true, RiskDeletesData: true, RiskChangesBilling: true, RiskConnectsAccounts: true}
	for _, key := range []RolePresetKey{PresetOperator, PresetSales, PresetAnalyst} {
		preset, ok := RolePresetByKey(key)
		if !ok {
			t.Fatalf("preset %q is missing", key)
		}
		perms, _ := preset.Permissions()
		for _, p := range perms {
			for _, risk := range RisksOf(p) {
				if forbidden[risk.Kind] {
					t.Errorf("preset %q grants %s (%s)", key, p.Key(), risk.Kind)
				}
			}
		}
	}
}

func TestTheOperatorSeesOnlyTheirOwnConversations(t *testing.T) {
	preset, _ := RolePresetByKey(PresetOperator)
	perms, _ := preset.Permissions()
	for _, p := range perms {
		if p.Resource == ResourceConversations && p.Action == ActionViewOthers {
			t.Fatal("an operator must not see colleagues' conversations")
		}
	}
	for _, want := range []PermissionEntry{
		{Resource: ResourceConversations, Action: ActionSend},
		{Resource: ResourceMedia, Action: ActionCreate},
	} {
		if !containsPermission(perms, want) {
			t.Errorf("operator is missing %s", want.Key())
		}
	}
}

func TestTheAnalystOnlyReads(t *testing.T) {
	preset, _ := RolePresetByKey(PresetAnalyst)
	perms, _ := preset.Permissions()
	allowed := map[Action]bool{ActionRead: true, ActionReadDetails: true, ActionViewOthers: true}
	for _, p := range perms {
		if allowed[p.Action] || (p.Resource == ResourceReports && p.Action == ActionCreate) || p.Resource == ResourceAIChat {
			continue
		}
		t.Errorf("analyst grants %s, which changes data", p.Key())
	}
}

func TestUnknownCapabilityMakesThePresetUnusable(t *testing.T) {
	broken := RolePreset{Key: "broken", Capabilities: []CapabilityKey{"inbox.view", "does.not_exist"}}
	if _, err := broken.Permissions(); err == nil {
		t.Fatal("a preset naming an unknown capability must not resolve to a partial set")
	}
}

func containsPermission(perms []PermissionEntry, want PermissionEntry) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}

func TestRoleNamesAreUniquePerWorkspaceIgnoringCaseAndSpaces(t *testing.T) {
	roles := []*CustomRole{{ID: "r1", Name: "Operador de atendimento"}, {ID: "r2", Name: "Gerente"}}
	cases := map[string]struct {
		name, except string
		taken        bool
	}{
		"same name":                   {"Operador de atendimento", "", true},
		"other case and spaces":       {"  operador DE atendimento ", "", true},
		"renaming the role itself":    {"Operador de atendimento", "r1", false},
		"a new name":                  {"Supervisor", "", false},
		"another role's name on edit": {"Gerente", "r1", true},
	}
	for name, tc := range cases {
		if got := RoleNameTaken(roles, tc.name, tc.except); got != tc.taken {
			t.Errorf("%s: RoleNameTaken = %v, want %v", name, got, tc.taken)
		}
	}
}

func TestALinkedRoleTakesItsPermissionsFromThePreset(t *testing.T) {
	role := &CustomRole{PresetKey: PresetOperator, Linked: true, Permissions: []PermissionEntry{{Resource: ResourceRoles, Action: ActionDelete}}}
	changed, err := role.SyncWithPreset()
	if err != nil || !changed {
		t.Fatalf("SyncWithPreset = %v, %v", changed, err)
	}
	preset, _ := RolePresetByKey(PresetOperator)
	want, _ := preset.Permissions()
	if !SamePermissions(role.Permissions, want) {
		t.Fatalf("permissions = %v, want the preset's", role.Permissions)
	}
	if again, _ := role.SyncWithPreset(); again {
		t.Fatal("an already synced role must report no change")
	}
}

func TestACopiedRoleKeepsItsOwnPermissions(t *testing.T) {
	own := []PermissionEntry{{Resource: ResourceConversations, Action: ActionRead}}
	role := &CustomRole{PresetKey: PresetOperator, Linked: false, Permissions: own}
	if changed, err := role.SyncWithPreset(); err != nil || changed || !SamePermissions(role.Permissions, own) {
		t.Fatalf("a copy must not follow the preset: changed=%v err=%v", changed, err)
	}
}

func TestALinkedRoleWithAnUnknownPresetIsRefused(t *testing.T) {
	role := &CustomRole{PresetKey: "retired", Linked: true}
	if _, err := role.SyncWithPreset(); !errors.Is(err, ErrUnknownRolePreset) {
		t.Fatalf("err = %v", err)
	}
	if err := (&CustomRole{Linked: true}).ValidatePreset(); !errors.Is(err, ErrUnknownRolePreset) {
		t.Fatalf("linked without a preset = %v", err)
	}
	if err := (&CustomRole{PresetKey: "nope"}).ValidatePreset(); !errors.Is(err, ErrUnknownRolePreset) {
		t.Fatalf("copy of an unknown preset = %v", err)
	}
	if err := (&CustomRole{}).ValidatePreset(); err != nil {
		t.Fatalf("a role built from scratch = %v", err)
	}
}

func TestSamePermissionsIgnoresOrder(t *testing.T) {
	a := []PermissionEntry{{Resource: ResourceConversations, Action: ActionRead}, {Resource: ResourceMedia, Action: ActionCreate}}
	b := []PermissionEntry{{Resource: ResourceMedia, Action: ActionCreate}, {Resource: ResourceConversations, Action: ActionRead}}
	if !SamePermissions(a, b) || SamePermissions(a, b[:1]) {
		t.Fatal("SamePermissions must compare as sets")
	}
}
