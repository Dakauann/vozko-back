package workspace

type FeatureKey string

type CapabilityKey string

type ScopeRule string

const (
	ScopeDepartments           ScopeRule = "departments"
	ScopeAssignedConversations ScopeRule = "assigned_conversations"
)

type Capability struct {
	Key          CapabilityKey     `json:"key"`
	Description  string            `json:"description"`
	Requires     []PermissionEntry `json:"requires,omitempty"`
	ManagersOnly bool              `json:"managersOnly,omitempty"`
	Screens      []Screen          `json:"screens,omitempty"`
}

type Feature struct {
	Key          FeatureKey   `json:"key"`
	Name         string       `json:"name"`
	Location     string       `json:"location"`
	Description  string       `json:"description"`
	Scopes       []ScopeRule  `json:"scopes,omitempty"`
	Capabilities []Capability `json:"capabilities"`
}

type FeatureUse struct {
	Feature    Feature
	Capability Capability
}

type Grants struct {
	manager bool
	held    map[string]bool
}

func NewGrants(role Role, platformAdmin bool, permissions []PermissionEntry) Grants {
	held := make(map[string]bool, len(permissions))
	for _, p := range permissions {
		held[p.Key()] = true
	}
	return Grants{manager: platformAdmin || role.CanManageMembers(), held: held}
}

func (g Grants) Manager() bool { return g.manager }

func (g Grants) Has(p PermissionEntry) bool {
	return g.manager || g.held[p.Key()]
}

func (c Capability) Missing(g Grants) []PermissionEntry {
	missing := make([]PermissionEntry, 0)
	for _, p := range c.Requires {
		if !g.Has(p) {
			missing = append(missing, p)
		}
	}
	return missing
}

func (c Capability) Allows(g Grants) bool {
	if g.manager {
		return true
	}
	return !c.ManagersOnly && len(c.Missing(g)) == 0
}

func (f Feature) Uses(rule ScopeRule) bool {
	for _, r := range f.Scopes {
		if r == rule {
			return true
		}
	}
	return false
}

func FeatureByKey(key FeatureKey) (Feature, bool) {
	for _, f := range Features {
		if f.Key == key {
			return f, true
		}
	}
	return Feature{}, false
}

func CapabilityForScreen(screen Screen) (FeatureUse, bool) {
	for _, f := range Features {
		for _, c := range f.Capabilities {
			for _, s := range c.Screens {
				if s == screen {
					return FeatureUse{Feature: f, Capability: c}, true
				}
			}
		}
	}
	return FeatureUse{}, false
}

func CapabilitiesUsing(p PermissionEntry) []FeatureUse {
	uses := make([]FeatureUse, 0)
	for _, f := range Features {
		for _, c := range f.Capabilities {
			for _, required := range c.Requires {
				if required.Key() == p.Key() {
					uses = append(uses, FeatureUse{Feature: f, Capability: c})
					break
				}
			}
		}
	}
	return uses
}
