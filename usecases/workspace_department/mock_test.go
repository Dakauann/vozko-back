package workspace_department_usecase

import (
	"sync"

	wd "vozko/domain/workspace/workspace_department"
)

type mockRepository struct {
	mu          sync.Mutex
	departments map[string]*wd.Department
	members     map[string]map[string]*wd.DepartmentMember
	nextErr     error
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		departments: make(map[string]*wd.Department),
		members:     make(map[string]map[string]*wd.DepartmentMember),
	}
}

func (m *mockRepository) failNext(err error) { m.nextErr = err }

func (m *mockRepository) consumeErr() error {
	err := m.nextErr
	m.nextErr = nil
	return err
}

func (m *mockRepository) CreateDepartment(dept *wd.Department) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return err
	}
	m.departments[dept.ID] = dept
	return nil
}

func (m *mockRepository) GetDepartmentByID(id string) (*wd.Department, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return nil, err
	}
	d, ok := m.departments[id]
	if !ok {
		return nil, wd.ErrDepartmentNotFound
	}
	copy := *d
	return &copy, nil
}

func (m *mockRepository) ListDepartments(workspaceID string) ([]wd.Department, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return nil, err
	}
	var result []wd.Department
	for _, d := range m.departments {
		if d.WorkspaceID == workspaceID {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *mockRepository) ListDepartmentsByIDs(ids []string) ([]wd.Department, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return nil, err
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	var result []wd.Department
	for _, d := range m.departments {
		if _, ok := idSet[d.ID]; ok {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *mockRepository) UpdateDepartment(dept *wd.Department) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return err
	}
	existing, ok := m.departments[dept.ID]
	if !ok {
		return wd.ErrDepartmentNotFound
	}
	existing.Name = dept.Name
	existing.Description = dept.Description
	return nil
}

func (m *mockRepository) DeleteDepartment(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return err
	}
	if _, ok := m.departments[id]; !ok {
		return wd.ErrDepartmentNotFound
	}
	delete(m.departments, id)
	delete(m.members, id)
	return nil
}

func (m *mockRepository) AddMember(dm *wd.DepartmentMember) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return err
	}
	if m.members[dm.DepartmentID] == nil {
		m.members[dm.DepartmentID] = make(map[string]*wd.DepartmentMember)
	}
	if _, ok := m.members[dm.DepartmentID][dm.MemberID]; ok {
		return wd.ErrDepartmentMemberExists
	}
	m.members[dm.DepartmentID][dm.MemberID] = dm
	return nil
}

func (m *mockRepository) RemoveMember(departmentID, memberID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return err
	}
	deptMembers, ok := m.members[departmentID]
	if !ok {
		return wd.ErrDepartmentMemberNotFound
	}
	if _, ok := deptMembers[memberID]; !ok {
		return wd.ErrDepartmentMemberNotFound
	}
	delete(deptMembers, memberID)
	return nil
}

func (m *mockRepository) ListMembers(departmentID string) ([]wd.DepartmentMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return nil, err
	}
	var result []wd.DepartmentMember
	for _, dm := range m.members[departmentID] {
		result = append(result, *dm)
	}
	return result, nil
}

func (m *mockRepository) GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return nil, err
	}
	var ids []string
	for deptID, deptMembers := range m.members {
		dept := m.departments[deptID]
		if dept == nil || dept.WorkspaceID != workspaceID {
			continue
		}
		for _, dm := range deptMembers {
			if dm.UserID == userID {
				ids = append(ids, deptID)
				break
			}
		}
	}
	return ids, nil
}

func (m *mockRepository) IsMember(departmentID, memberID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.consumeErr(); err != nil {
		return false, err
	}
	deptMembers, ok := m.members[departmentID]
	if !ok {
		return false, nil
	}
	_, exists := deptMembers[memberID]
	return exists, nil
}
