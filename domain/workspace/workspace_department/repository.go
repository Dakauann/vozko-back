package workspace_department

type Repository interface {
	CreateDepartment(dept *Department) error
	GetDepartmentByID(id string) (*Department, error)
	ListDepartments(workspaceID string) ([]Department, error)
	ListDepartmentsByIDs(ids []string) ([]Department, error)
	UpdateDepartment(dept *Department) error
	DeleteDepartment(id string) error

	AddMember(dm *DepartmentMember) error
	RemoveMember(departmentID, memberID string) error
	ListMembers(departmentID string) ([]DepartmentMember, error)
	GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error)
	IsMember(departmentID, memberID string) (bool, error)

	// ListWorkingHours returns the departments in these workspaces that have
	// their OWN schedule, in one query.
	//
	// Departments without an override are absent rather than nil-valued, so
	// "inherits the workspace" stays distinguishable from "has an empty policy".
	ListWorkingHours(workspaceIDs []string) ([]DepartmentSchedule, error)
}
