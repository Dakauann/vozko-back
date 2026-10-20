package workspace

import "testing"

func TestMercadoLivreAccountsMirrorInstagramAccess(t *testing.T) {
	if ResourceMercadoLivreAccounts != "mercadolivre_accounts" || !ResourceMercadoLivreAccounts.IsValid() {
		t.Fatalf("resource = %q", ResourceMercadoLivreAccounts)
	}
	risks := map[Action][]RiskKind{}
	for _, def := range ResourceActions[ResourceMercadoLivreAccounts] {
		if def.Description == "" {
			t.Errorf("%s has no description", def.ActionName)
		}
		risks[def.ActionName] = def.Risks
	}
	for _, action := range []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete} {
		if _, ok := risks[action]; !ok {
			t.Errorf("mercadolivre_accounts misses %s", action)
		}
	}
	for _, action := range []Action{ActionCreate, ActionDelete} {
		if !hasRisk(risks[action], RiskConnectsAccounts) {
			t.Errorf("%s connects accounts and must say so", action)
		}
	}
	if !hasRisk(risks[ActionUpdate], RiskContactsCustomers) {
		t.Error("update answers buyers and must say so")
	}

	feature, ok := FeatureByKey("mercadolivre")
	if !ok {
		t.Fatal("no mercadolivre feature")
	}
	keys := map[CapabilityKey]Capability{}
	for _, c := range feature.Capabilities {
		keys[c.Key] = c
	}
	for _, key := range []CapabilityKey{"mercadolivre.view", "mercadolivre.connect", "mercadolivre.manage", "mercadolivre.disconnect"} {
		if _, ok := keys[key]; !ok {
			t.Errorf("missing capability %s", key)
		}
	}
	for screen, want := range map[Screen]CapabilityKey{
		ScreenMercadoLivreAccounts: "mercadolivre.view",
		ScreenMercadoLivreAccount:  "mercadolivre.view",
		ScreenMercadoLivreConnect:  "mercadolivre.connect",
	} {
		use, ok := CapabilityForScreen(screen)
		if !ok || use.Capability.Key != want {
			t.Errorf("screen %s opened by %v, want %s", screen, use.Capability.Key, want)
		}
	}
	if params := screenParams[ScreenMercadoLivreAccount]; len(params) != 1 || params[0] != "accountId" {
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
	for _, key := range []CapabilityKey{"mercadolivre.view", "mercadolivre.manage"} {
		if held["instagram.view"] && !held[key] {
			t.Errorf("marketing carries Instagram but not %s", key)
		}
	}
}

func hasRisk(risks []RiskKind, want RiskKind) bool {
	for _, r := range risks {
		if r == want {
			return true
		}
	}
	return false
}
