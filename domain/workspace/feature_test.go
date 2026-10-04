package workspace

import (
	"strings"
	"testing"
)

var inertPermissions = map[string]bool{
	"workflows:read_details": true,
	"media:delete":           true,
	"leads:delete":           true,
}

var openToEveryMember = map[CapabilityKey]bool{
	"team.workspace": true,
}

func TestFeaturesAreFullyDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Features {
		if f.Key == "" || f.Name == "" || f.Location == "" || f.Description == "" || len(f.Capabilities) == 0 {
			t.Errorf("feature %q is incomplete", f.Key)
		}
		if seen[string(f.Key)] {
			t.Errorf("feature %q is declared twice", f.Key)
		}
		seen[string(f.Key)] = true
		for _, c := range f.Capabilities {
			if !strings.HasPrefix(string(c.Key), string(f.Key)+".") {
				t.Errorf("capability %q must be prefixed by its feature %q", c.Key, f.Key)
			}
			if seen[string(c.Key)] {
				t.Errorf("capability %q is declared twice", c.Key)
			}
			seen[string(c.Key)] = true
			if c.Description == "" {
				t.Errorf("capability %q has no description", c.Key)
			}
			if len(c.Requires) == 0 && !c.ManagersOnly && !openToEveryMember[c.Key] {
				t.Errorf("capability %q grants access to everyone; declare what it requires", c.Key)
			}
		}
	}
}

func TestCapabilitiesRequireRealPermissionsWithTheirDependencies(t *testing.T) {
	for _, f := range Features {
		for _, c := range f.Capabilities {
			required := map[string]bool{}
			for _, p := range c.Requires {
				required[p.Key()] = true
			}
			for _, p := range c.Requires {
				if !ValidActionForResource(p.Resource, p.Action) {
					t.Errorf("capability %q requires %s, which is not in the permission catalog", c.Key, p.Key())
				}
				for _, dep := range RequirementsOf(p) {
					if !required[dep.Key()] {
						t.Errorf("capability %q requires %s but not its dependency %s", c.Key, p.Key(), dep.Key())
					}
				}
			}
		}
	}
}

func TestEveryPermissionIsExplainedByACapability(t *testing.T) {
	for resource, defs := range ResourceActions {
		for _, def := range defs {
			p := PermissionEntry{Resource: resource, Action: def.ActionName}
			used := len(CapabilitiesUsing(p)) > 0
			switch {
			case inertPermissions[p.Key()] && used:
				t.Errorf("%s is now used; remove it from the inert list", p.Key())
			case !inertPermissions[p.Key()] && !used:
				t.Errorf("%s is not used by any capability; map it to the feature it unlocks", p.Key())
			}
		}
	}
}

func TestEveryScreenIsOpenedByExactlyOneCapability(t *testing.T) {
	owners := map[Screen][]CapabilityKey{}
	for _, f := range Features {
		for _, c := range f.Capabilities {
			for _, s := range c.Screens {
				if !s.Valid() {
					t.Errorf("capability %q opens unknown screen %q", c.Key, s)
				}
				owners[s] = append(owners[s], c.Key)
			}
		}
	}
	for _, s := range Screens() {
		if len(owners[s]) != 1 {
			t.Errorf("screen %q is opened by %v; it needs exactly one capability", s, owners[s])
		}
	}
}

var formScreenCapabilities = map[CapabilityKey]bool{
	"agents.create":               true,
	"agents.edit":                 true,
	"knowledge_bases.create":      true,
	"knowledge_bases.edit":        true,
	"workflows.create":            true,
	"whatsapp_campaigns.create":   true,
	"whatsapp_campaigns.edit":     true,
	"whatsapp_templates.create":   true,
	"whatsapp_templates.send":     true,
	"business_phones.connect":     true,
	"unofficial_numbers.connect":  true,
	"unofficial_campaigns.create": true,
	"unofficial_campaigns.edit":   true,
	"instagram.connect":           true,
	"telegram.connect":            true,
	"webchat.create":              true,
	"facebook.connect":            true,
	"ads.create":                  true,
	"links.create":                true,
	"links.edit":                  true,
	"issues.create":               true,
	"audience.alerts":             true,
}

func TestSeeingAScreenNeverRequiresAnActionPermission(t *testing.T) {
	readOnly := map[Action]bool{ActionRead: true, ActionReadDetails: true}
	for _, f := range Features {
		for _, c := range f.Capabilities {
			if len(c.Screens) == 0 || formScreenCapabilities[c.Key] {
				continue
			}
			if c.ManagersOnly {
				t.Errorf("%q opens screens but is managers only; list and detail screens must open for members who can read", c.Key)
			}
			for _, p := range c.Requires {
				if !readOnly[p.Action] {
					t.Errorf("%q opens screens but requires %s; seeing must never depend on an action permission", c.Key, p.Key())
				}
			}
		}
	}
}

func TestFeaturesUseKnownScopes(t *testing.T) {
	for _, f := range Features {
		for _, rule := range f.Scopes {
			if rule != ScopeDepartments && rule != ScopeAssignedConversations {
				t.Errorf("feature %q uses unknown scope %q", f.Key, rule)
			}
		}
	}
}

func TestKanbanNeedsConversationsAndStagesAndMovingNeedsAssign(t *testing.T) {
	board, ok := FeatureByKey("crm_board")
	if !ok {
		t.Fatal("crm_board feature missing")
	}
	member := NewGrants(RoleMember, false, []PermissionEntry{
		{Resource: ResourceConversations, Action: ActionRead},
		{Resource: ResourceStages, Action: ActionRead},
	})
	view, move := capabilityOf(t, board, "crm_board.view"), capabilityOf(t, board, "crm_board.move_stage")
	if !view.Allows(member) {
		t.Errorf("reading conversations and stages must open the board, missing %v", view.Missing(member))
	}
	missing := move.Missing(member)
	if move.Allows(member) || len(missing) != 1 || missing[0].Key() != "stages:assign" {
		t.Errorf("moving cards must be blocked only by stages:assign, got %v", missing)
	}
}

func TestManagersPassEveryCapabilityAndMembersNeverPassManagerOnlyOnes(t *testing.T) {
	reserved := Capability{Key: "x.reserved", ManagersOnly: true}
	gated := Capability{Key: "x.gated", Requires: []PermissionEntry{{Resource: ResourceLeads, Action: ActionRead}}}
	everything := NewGrants(RoleMember, false, []PermissionEntry{{Resource: ResourceLeads, Action: ActionRead}})
	for _, g := range []Grants{NewGrants(RoleOwner, false, nil), NewGrants(RoleAdmin, false, nil), NewGrants(RoleMember, true, nil)} {
		if !reserved.Allows(g) || !gated.Allows(g) {
			t.Error("owners, admins and platform admins pass every capability")
		}
	}
	if reserved.Allows(everything) {
		t.Error("a member never passes a managers-only capability")
	}
	if !gated.Allows(everything) {
		t.Error("a member holding every requirement passes")
	}
}

func TestScreenLookupAndPermissionUsage(t *testing.T) {
	use, ok := CapabilityForScreen(ScreenFunnels)
	if !ok || use.Capability.Key != "funnels.view" {
		t.Fatalf("funnels screen must be opened by funnels.view, got %+v", use.Capability.Key)
	}
	uses := CapabilitiesUsing(PermissionEntry{Resource: ResourceStages, Action: ActionAssign})
	keys := map[CapabilityKey]bool{}
	for _, u := range uses {
		keys[u.Capability.Key] = true
	}
	if !keys["crm_board.move_stage"] || !keys["inbox.stage"] {
		t.Errorf("stages:assign must explain the kanban drag and the stage picker, got %v", keys)
	}
}

func capabilityOf(t *testing.T, f Feature, key CapabilityKey) Capability {
	t.Helper()
	for _, c := range f.Capabilities {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("capability %q missing from %q", key, f.Key)
	return Capability{}
}

func TestScreensTakeAtMostOneParam(t *testing.T) {
	for _, s := range Screens() {
		if len(s.Params()) > 1 {
			t.Errorf("screen %q takes %v; navigation carries a single id", s, s.Params())
		}
	}
}
