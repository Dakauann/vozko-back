package workspace_department

type DepartmentFilter struct {
	IsOwnerOrAdmin bool

	DepartmentIDs []string

	SelectedDepartmentID *string

	WorkspaceHasDepartments bool
}

func (f *DepartmentFilter) ShouldFilter() bool {
	if f == nil || f.IsOwnerOrAdmin {
		return false
	}
	return f.hasWorkspaceDepartments()
}

func (f *DepartmentFilter) EffectiveDepartmentIDs() []string {
	if f == nil {
		return nil
	}
	if f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != "" {
		return []string{*f.SelectedDepartmentID}
	}
	if f.IsOwnerOrAdmin || !f.hasWorkspaceDepartments() {
		return nil
	}
	return f.DepartmentIDs
}

func (f *DepartmentFilter) DepartmentIDForCreation() (string, error) {
	if f == nil || f.IsOwnerOrAdmin {

		if f != nil && f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != "" {
			return *f.SelectedDepartmentID, nil
		}
		return "", nil
	}
	if !f.hasWorkspaceDepartments() {
		return "", nil
	}
	if f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != "" {
		return *f.SelectedDepartmentID, nil
	}
	if len(f.DepartmentIDs) == 0 {
		return "", ErrDepartmentAccessDenied
	}
	if len(f.DepartmentIDs) == 1 {
		return f.DepartmentIDs[0], nil
	}
	return "", ErrDepartmentRequired
}

func (f *DepartmentFilter) hasWorkspaceDepartments() bool {
	if f == nil {
		return false
	}
	if f.WorkspaceHasDepartments {
		return true
	}
	if len(f.DepartmentIDs) > 0 {
		return true
	}
	return f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != ""
}

// BlockedByMissingDepartment is the state that keeps being mistaken for a bug.
//
// Departments are a SCOPE, not a permission. A workspace with none filters on
// permissions alone, so a member with a full role sees everything. The moment
// one department exists the rule changes for every non-admin: they see the
// conversations of the departments they belong to, and a member who belongs to
// none belongs to nothing, so the query matches no rows at all.
//
// That member's role can be completely unrestricted and it changes nothing,
// which is exactly why the screen has to say so. Left unexplained it renders as
// "no conversations yet", and the workspace looks broken rather than
// misconfigured.
func (f *DepartmentFilter) BlockedByMissingDepartment() bool {
	if f == nil || f.IsOwnerOrAdmin {
		return false
	}
	return f.WorkspaceHasDepartments && len(f.DepartmentIDs) == 0
}

// Scope is what a member needs to be told about their own visibility. It says
// nothing about anyone else: how many departments exist, who is in them and
// what they hold are all absent by construction, so this is safe to hand to
// the least privileged member in the workspace.
type Scope struct {
	// WorkspaceUsesDepartments reports whether the department rule is in force
	// at all. Without it "you are in no department" is not a problem worth
	// mentioning, and saying it would be noise.
	WorkspaceUsesDepartments bool `json:"workspaceUsesDepartments"`
	// MemberDepartmentCount is how many the caller belongs to. A count, never
	// the names: which departments exist is not this member's business.
	MemberDepartmentCount int `json:"memberDepartmentCount"`
	// RestrictedToOwnDepartments is true when the caller sees only their own
	// departments' work. False for owners and admins.
	RestrictedToOwnDepartments bool `json:"restrictedToOwnDepartments"`
	// BlockedByMissingDepartment is the actionable one: restricted, and in no
	// department, therefore seeing nothing.
	BlockedByMissingDepartment bool `json:"blockedByMissingDepartment"`
}

// ScopeFor describes the caller's own visibility.
func ScopeFor(f *DepartmentFilter) Scope {
	if f == nil {
		return Scope{}
	}
	return Scope{
		WorkspaceUsesDepartments:   f.hasWorkspaceDepartments(),
		MemberDepartmentCount:      len(f.DepartmentIDs),
		RestrictedToOwnDepartments: f.ShouldFilter(),
		BlockedByMissingDepartment: f.BlockedByMissingDepartment(),
	}
}
