package workspace

import "testing"

func TestTheCallListScreensAreOpenedByCallListsView(t *testing.T) {
	for _, screen := range []Screen{ScreenCallLists, ScreenCallListDetail} {
		use, ok := CapabilityForScreen(screen)
		if !ok || use.Capability.Key != "call_lists.view" {
			t.Fatalf("%s must be opened by call_lists.view, got %q", screen, use.Capability.Key)
		}
	}
	if params := ScreenCallListDetail.Params(); len(params) != 1 || params[0] != "listId" {
		t.Fatalf("call_list_detail params = %v, want [listId]", params)
	}
	if params := ScreenCallLists.Params(); len(params) != 0 {
		t.Fatalf("call_lists params = %v, want none", params)
	}
}

func TestTheCallListCapabilities(t *testing.T) {
	read := PermissionEntry{Resource: ResourceCallLists, Action: ActionRead}
	cases := []struct {
		key      CapabilityKey
		requires []PermissionEntry
	}{
		{"call_lists.view", []PermissionEntry{read}},
		{"call_lists.manage", []PermissionEntry{read, {Resource: ResourceCallLists, Action: ActionManage}, {Resource: ResourceLeads, Action: ActionRead}}},
		{"call_lists.work", []PermissionEntry{read, {Resource: ResourceSIPTrunks, Action: ActionRead}, {Resource: ResourceSIPTrunks, Action: ActionCall}, {Resource: ResourceCallSession, Action: ActionUse}}},
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

func TestManagingCallListsNeedsReadingThem(t *testing.T) {
	for _, def := range ResourceActions[ResourceCallLists] {
		if def.ActionName != ActionManage {
			continue
		}
		if !containsPermission(def.Requires, PermissionEntry{Resource: ResourceCallLists, Action: ActionRead}) {
			t.Fatal("call_lists:manage must require call_lists:read")
		}
		return
	}
	t.Fatal("call_lists:manage is not a call list action")
}

func TestOperatorsWorkCallListsAndManagersAlsoManageThem(t *testing.T) {
	cases := []struct {
		preset RolePresetKey
		has    []CapabilityKey
		lacks  []CapabilityKey
	}{
		{PresetOperator, []CapabilityKey{"call_lists.view", "call_lists.work"}, []CapabilityKey{"call_lists.manage"}},
		{PresetSupervisor, []CapabilityKey{"call_lists.view", "call_lists.work"}, []CapabilityKey{"call_lists.manage"}},
		{PresetManager, []CapabilityKey{"call_lists.view", "call_lists.work", "call_lists.manage"}, nil},
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
