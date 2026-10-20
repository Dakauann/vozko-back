package workspace

import "testing"

func TestTikTokAccountsMirrorInstagramAccess(t *testing.T) {
	if ResourceTikTokAccounts != "tiktok_accounts" || !ResourceTikTokAccounts.IsValid() {
		t.Fatalf("resource = %q", ResourceTikTokAccounts)
	}
	risks := map[Action][]RiskKind{}
	for _, def := range ResourceActions[ResourceTikTokAccounts] {
		if def.Description == "" {
			t.Errorf("%s has no description", def.ActionName)
		}
		risks[def.ActionName] = def.Risks
	}
	for _, action := range []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete} {
		if _, ok := risks[action]; !ok {
			t.Errorf("tiktok_accounts misses %s", action)
		}
	}
	for _, action := range []Action{ActionCreate, ActionDelete} {
		if !hasRisk(risks[action], RiskConnectsAccounts) {
			t.Errorf("%s connects accounts and must say so", action)
		}
	}
	if !hasRisk(risks[ActionUpdate], RiskContactsCustomers) {
		t.Error("update replies to comments and must say so")
	}

	feature, ok := FeatureByKey("tiktok")
	if !ok {
		t.Fatal("no tiktok feature")
	}
	keys := map[CapabilityKey]Capability{}
	for _, c := range feature.Capabilities {
		keys[c.Key] = c
	}
	for _, key := range []CapabilityKey{"tiktok.view", "tiktok.connect", "tiktok.manage", "tiktok.disconnect"} {
		if _, ok := keys[key]; !ok {
			t.Errorf("missing capability %s", key)
		}
	}
	for screen, want := range map[Screen]CapabilityKey{
		ScreenTikTokAccounts: "tiktok.view",
		ScreenTikTokAccount:  "tiktok.view",
		ScreenTikTokConnect:  "tiktok.connect",
	} {
		use, ok := CapabilityForScreen(screen)
		if !ok || use.Capability.Key != want {
			t.Errorf("screen %s opened by %v, want %s", screen, use.Capability.Key, want)
		}
	}
	if params := screenParams[ScreenTikTokAccount]; len(params) != 1 || params[0] != "accountId" {
		t.Errorf("account screen params = %v", params)
	}

	marketing, ok := RolePresetByKey(PresetMarketing)
	if !ok {
		t.Fatal("no marketing preset")
	}
	held := map[CapabilityKey]bool{}
	for _, c := range marketing.Capabilities {
		held[c] = true
	}
	for _, key := range []CapabilityKey{"tiktok.view", "tiktok.manage"} {
		if held["instagram.view"] && !held[key] {
			t.Errorf("marketing carries Instagram but not %s", key)
		}
	}
}
