package tooltest

import wd "vozko/domain/workspace/workspace_department"

const SalesDeptUUID = "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"

type DepartmentBook map[string]*wd.Department

func (d DepartmentBook) Get(workspaceID, id string) (*wd.Department, error) {
	if dept, ok := d[id]; ok && dept.WorkspaceID == workspaceID {
		return dept, nil
	}
	return nil, wd.ErrDepartmentNotFound
}

type FakeListDepartments struct{ Depts []wd.Department }

func (f FakeListDepartments) Execute(string) ([]wd.Department, error) { return f.Depts, nil }
