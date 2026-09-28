package workspace

import "strings"

type ScopeFindingKind string

const (
	ScopeNoDepartment   ScopeFindingKind = "no_department"
	ScopeOwnDepartments ScopeFindingKind = "own_departments"
	ScopeAssignedOnly   ScopeFindingKind = "assigned_only"
)

type MemberScope struct {
	WorkspaceUsesDepartments bool
	DepartmentCount          int
	DepartmentNames          []string
}

type ScopeFinding struct {
	Kind        ScopeFindingKind `json:"kind"`
	Description string           `json:"description"`
}

type CapabilityStatus struct {
	Key          CapabilityKey     `json:"key"`
	Description  string            `json:"description"`
	Allowed      bool              `json:"allowed"`
	Missing      []PermissionEntry `json:"missing,omitempty"`
	ManagersOnly bool              `json:"managersOnly,omitempty"`
	Screens      []Screen          `json:"screens,omitempty"`
}

type FeatureDiagnosis struct {
	Feature      FeatureKey         `json:"feature"`
	Name         string             `json:"name"`
	Location     string             `json:"location"`
	FullAccess   bool               `json:"fullAccess"`
	Capabilities []CapabilityStatus `json:"capabilities"`
	Scope        []ScopeFinding     `json:"scope,omitempty"`
}

func (f Feature) Diagnose(g Grants, scope MemberScope) FeatureDiagnosis {
	statuses := make([]CapabilityStatus, 0, len(f.Capabilities))
	for _, c := range f.Capabilities {
		status := CapabilityStatus{Key: c.Key, Description: c.Description, Allowed: c.Allows(g), ManagersOnly: c.ManagersOnly, Screens: c.Screens}
		if !g.Manager() {
			if missing := c.Missing(g); len(missing) > 0 {
				status.Missing = missing
			}
		}
		statuses = append(statuses, status)
	}
	return FeatureDiagnosis{
		Feature:      f.Key,
		Name:         f.Name,
		Location:     f.Location,
		FullAccess:   g.Manager(),
		Capabilities: statuses,
		Scope:        f.scopeFindings(g, scope),
	}
}

func (f Feature) scopeFindings(g Grants, scope MemberScope) []ScopeFinding {
	if g.Manager() {
		return nil
	}
	var findings []ScopeFinding
	if f.Uses(ScopeDepartments) && scope.WorkspaceUsesDepartments {
		findings = append(findings, departmentFinding(scope))
	}
	if f.Uses(ScopeAssignedConversations) && !g.Has(PermissionEntry{Resource: ResourceConversations, Action: ActionViewOthers}) {
		findings = append(findings, ScopeFinding{
			Kind:        ScopeAssignedOnly,
			Description: "Sem a permissão de visualizar conversas de outros membros, aparecem apenas as conversas atribuídas ao próprio membro.",
		})
	}
	return findings
}

func departmentFinding(scope MemberScope) ScopeFinding {
	if scope.DepartmentCount == 0 {
		return ScopeFinding{
			Kind:        ScopeNoDepartment,
			Description: "O workspace usa departamentos e o membro não participa de nenhum. Os itens separados por departamento não aparecem para ele até que seja incluído em um departamento.",
		}
	}
	description := "Aparecem apenas os itens dos departamentos do membro."
	if len(scope.DepartmentNames) > 0 {
		description = "Aparecem apenas os itens dos departamentos do membro: " + strings.Join(scope.DepartmentNames, ", ") + "."
	}
	return ScopeFinding{Kind: ScopeOwnDepartments, Description: description}
}
