package branch

// Repository is the persistence port for branches. It lives in the domain and
// is implemented by infra/repositories/branch (GORM). Mirrors sip_trunk.Repository.
type Repository interface {
	Create(b *Branch) error
	Update(b *Branch) error
	Delete(id string) error

	FindByID(id string) (*Branch, error)
	// FindBySIPUser resolves the branch a REGISTER/INVITE for a sip_user maps to
	// within a workspace. Returns ErrBranchNotFound when absent. Used by the
	// Phase 1 registrar and uniqueness checks.
	FindBySIPUser(workspaceID, sipUser string) (*Branch, error)
	FindByWorkspace(workspaceID string, page, pageSize int) ([]*Branch, int64, error)
	// FindByUser returns every branch bound to a member (multi-device is a single
	// branch with several contacts, but a member could hold more than one branch).
	FindByUser(workspaceID, userID string) ([]*Branch, error)
	// FindByGlobalSIPUser resolves a branch from a bare SIP username, with no
	// workspace context, as the registrar must when a REGISTER arrives. Requires
	// sip_user to be globally unique (single-VPS model). Returns ErrBranchNotFound
	// when absent and ErrBranchSIPUserTaken if somehow ambiguous (fail safe).
	FindByGlobalSIPUser(sipUser string) (*Branch, error)

	CountByWorkspace(workspaceID string) (int64, error)

	// UpdateRegistrationStatus is a narrow write used by the Phase 1 registrar's
	// hot path so it never round-trips the whole entity.
	UpdateRegistrationStatus(id string, status RegistrationStatus) error

	// ResetLiveRegistrations marks every branch currently flagged `registered` as
	// `expired`. The registrar calls it on boot because the live AOR bindings are
	// in-process and are lost on restart: without this the dashboard would keep
	// claiming a branch is online while the routing layer can no longer reach it,
	// until the phone's next refresh REGISTER. Returns the number of rows reset.
	ResetLiveRegistrations() (int64, error)
}

// MemberDirectory is the narrow slice of the workspace membership lookup the
// branch use cases need to bind a branch to a real member and stamp MemberID.
// Implemented by the workspace repository (adapted in the container) so the
// branch use cases don't depend on the whole workspace domain surface.
type MemberDirectory interface {
	// ResolveMember returns the membership id for a (workspace, user) pair, or
	// ErrBranchMemberNotFound when the user is not a member of the workspace.
	ResolveMember(workspaceID, userID string) (memberID string, err error)
}
