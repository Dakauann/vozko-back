package workspace

type CreateWorkspaceUseCase interface {
	Execute(ownerID, name string) (*Workspace, error)
}

type GetWorkspaceUseCase interface {
	Execute(userID, role, workspaceID string) (*Workspace, error)
}

type ListWorkspacesUseCase interface {
	Execute(userID, role, search, memberEmail string, page, pageSize int) (interface{}, error)
}

type UpdateWorkspaceUseCase interface {
	Execute(userID, workspaceID, callerRole string, input UpdateWorkspaceInput) (*Workspace, error)
}

type InviteMemberUseCase interface {
	Execute(inviterID, workspaceID, callerRole string, input InviteMemberInput) (*Invite, error)
}

type AcceptInviteUseCase interface {
	Execute(userID, userEmail, token string) (*Member, error)
}

type DeclineInviteUseCase interface {
	Execute(userID, userEmail, inviteID string) error
}

type CancelInviteUseCase interface {
	Execute(actorID, workspaceID, callerRole, inviteID string) error
}

type ListInvitesUseCase interface {
	Execute(email string) ([]*Invite, error)
}

type ListWorkspaceInvitesUseCase interface {
	Execute(userID, workspaceID, callerRole string) ([]*Invite, error)
}

type RemoveMemberUseCase interface {
	Execute(actorID, workspaceID, memberUserID, callerRole string) error
}

type UpdateMemberRoleUseCase interface {
	Execute(actorID, workspaceID, memberUserID, callerRole string, role Role) (*Member, error)
}

type ListMembersUseCase interface {
	Execute(userID, workspaceID, callerRole string) ([]*Member, error)
}

type ListMembersPaginatedUseCase interface {
	Execute(workspaceID string, page, pageSize int) ([]*Member, int64, error)
}

// MemberVisibilityScope describes which workspace members a given caller is
// allowed to see. Restrict=false means "all members"; Restrict=true means
// "only members in DepartmentIDs" (an empty DepartmentIDs means the caller
// belongs to no department and may therefore see only themselves).
type MemberVisibilityScope struct {
	Restrict      bool     `json:"restrict"`
	DepartmentIDs []string `json:"departmentIds,omitempty"`
	// IncludeAdmins lets a department-scoped caller also reach workspace
	// owners/admins (so escalation works) when the workspace lets admins
	// participate in roulette (SkipAdminAssignment=false). It is only meaningful
	// when Restrict is true.
	IncludeAdmins bool `json:"includeAdmins,omitempty"`
}

// MemberVisibilityUseCase is the single source of truth for member-visibility
// policy: owner/admin (or platform admin) see everyone; a workspace with no
// departments imposes no scope; members:view_others widens a regular member to
// all departments; otherwise a regular member is scoped to their own
// department(s), or to just themselves when they belong to none.
type MemberVisibilityUseCase interface {
	Scope(userID, workspaceID string, isPlatformAdmin bool) (MemberVisibilityScope, error)
	CanView(callerUserID, targetUserID, workspaceID string, isPlatformAdmin bool) (bool, error)
}

// DepartmentRef is a lightweight department identity used to bucket members in
// the assignable-members picker.
type DepartmentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AssignableMember is a workspace member annotated with the departments (within
// the caller's visible scope) it belongs to, so the picker can group by
// department. Departments is empty when the workspace has no departments.
type AssignableMember struct {
	*Member
	Departments []DepartmentRef `json:"departments"`
}

type ListAssignableMembersUseCase interface {
	Execute(userID, workspaceID string, isPlatformAdmin bool, search string, page, pageSize int) ([]*AssignableMember, int64, error)
}

type SetMemberPermissionsUseCase interface {
	Execute(actorID, workspaceID, memberUserID, callerRole string, input SetPermissionsInput) ([]*Permission, error)
}

type GetMemberPermissionsUseCase interface {
	Execute(actorID, workspaceID, memberUserID, callerRole string) ([]*Permission, error)
}

type ListResourcePermissionsUseCase interface {
	Execute() []ResourcePermissionInfo
}

type CheckAccessUseCase interface {
	Execute(userID, workspaceID string, resource Resource, action Action) error
}

type EnsureDefaultWorkspaceUseCase interface {
	Execute(userID, email, referralCode string) (*Workspace, error)
}

type AssignResourceUseCase interface {
	Execute(actorID, workspaceID, callerRole string, input AssignResourceInput) (*ResourceAssignment, error)
}

type UnassignResourceUseCase interface {
	Execute(actorID, workspaceID, resourceType, resourceID, memberUserID, callerRole string) error
}

type ListResourceAssignmentsUseCase interface {
	Execute(actorID, workspaceID, callerRole, resourceType, resourceID string) ([]*ResourceAssignment, error)
}

type CheckResourceAccessUseCase interface {
	Execute(userID, workspaceID string, resource Resource, action Action, resourceID string) error
}

type UpdateWorkspaceInput struct {
	Name *string `json:"name,omitempty"`
}

type InviteMemberInput struct {
	Email         string   `json:"email"`
	Role          Role     `json:"role"`
	RoleID        string   `json:"roleId,omitempty"`
	DepartmentIDs []string `json:"departmentIds,omitempty"`
}

type SetPermissionsInput struct {
	Permissions []PermissionEntry `json:"permissions"`
}

type PermissionEntry struct {
	Resource Resource `json:"resource"`
	Action   Action   `json:"action"`
}

type ResourcePermissionInfo struct {
	Resource           Resource                     `json:"resource"`
	Actions            []Action                     `json:"actions"`
	ActionDescriptions map[string]string            `json:"actionDescriptions"`
	Dependencies       map[string][]PermissionEntry `json:"dependencies,omitempty"`
}

type AssignResourceInput struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	MemberUserID string `json:"memberUserId"`
}
