package workspace

type Repository interface {
	WithTx(tx interface{}) Repository
	CreateWorkspace(ws *Workspace) error
	GetWorkspaceByID(id string) (*Workspace, error)
	GetDefaultWorkspace(ownerID string) (*Workspace, error)
	ListWorkspacesByUser(userID, search, memberEmail string) ([]*Workspace, error)
	ListAllWorkspaces(search, memberEmail string, page, pageSize int) ([]*Workspace, int64, error)
	ListAllWorkspaceIDs() ([]string, error)
	CountMembersByWorkspaceIDs(workspaceIDs []string) (map[string]int, error)
	ListMembersPaginated(workspaceID string, page, pageSize int) ([]*Member, int64, error)
	UpdateWorkspace(ws *Workspace) error
	TransferOwnership(workspaceID, newOwnerID string) error
	DetachUserAuthoredRefs(userID string) error

	AddMember(member *Member) error
	GetMember(workspaceID, userID string) (*Member, error)
	GetMemberByID(memberID string) (*Member, error)
	ListMembers(workspaceID string) ([]*Member, error)
	ListAssignableMembers(workspaceID, search string, restrict bool, departmentIDs []string, includeAdmins bool, selfUserID string, page, pageSize int) ([]*Member, int64, error)
	ListMemberDepartments(workspaceID string, memberIDs []string, restrictToDeptIDs []string) (map[string][]DepartmentRef, error)
	UpdateMemberRole(memberID string, role Role) error
	UpdateMemberRoleID(memberID string, roleID string) error
	RemoveMember(memberID string) error

	AddPermission(perm *Permission) error
	RemovePermission(memberID string, resource Resource, action Action) error
	GetPermissions(memberID string) ([]*Permission, error)
	HasPermission(memberID string, resource Resource, action Action) (bool, error)
	SetPermissions(memberID string, permissions []*Permission) error

	CreateInvite(invite *Invite) error
	GetInviteByID(id string) (*Invite, error)
	GetInviteByToken(token string) (*Invite, error)
	ListInvitesByWorkspace(workspaceID string) ([]*Invite, error)
	ListInvitesByEmail(email string) ([]*Invite, error)
	UpdateInviteStatus(inviteID string, status InviteStatus) error
	PendingInviteExists(workspaceID, email string) (bool, error)

	GetWorkspaceIDForResource(resourceTable, resourceID string) (string, error)

	AssignResource(assignment *ResourceAssignment) error
	UnassignResource(workspaceID string, resourceType Resource, resourceID, memberID string) error
	ListAssignmentsByResource(workspaceID string, resourceType Resource, resourceID string) ([]*ResourceAssignment, error)
	ListAssignmentsByMember(memberID string, resourceType Resource) ([]*ResourceAssignment, error)
	IsResourceAssignedToMember(workspaceID string, resourceType Resource, resourceID, memberID string) (bool, error)
	HasAnyAssignments(workspaceID string, resourceType Resource, resourceID string) (bool, error)
}
