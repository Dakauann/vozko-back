package workspace_department

type ScopedDepartmentsUseCase interface {
	Get(workspaceID, departmentID string) (*Department, error)
	Update(workspaceID, departmentID string, input UpdateDepartmentInput) (*Department, error)
	Delete(workspaceID, departmentID string) error
	ListMembers(workspaceID, departmentID string) ([]DepartmentMember, error)
	AddMember(workspaceID, departmentID string, input AddMemberInput) (*DepartmentMember, error)
	RemoveMember(workspaceID, departmentID, memberID string) error
}
