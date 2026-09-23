package attendance_target

type Selector struct {
	DepartmentID string
	MemberID     string
}

type Resolution struct {
	Value    float64
	Currency string
	Scope    Scope
	ScopeID  string
}

func Resolve(targets []Target, metricKey string, selector Selector) (Resolution, bool) {
	var workspace, department, member *Target

	for i := range targets {
		target := targets[i]
		if target.MetricKey != metricKey {
			continue
		}
		switch target.Scope {
		case ScopeMember:
			if selector.MemberID != "" && target.ScopeID == selector.MemberID {
				member = &targets[i]
			}
		case ScopeDepartment:
			if selector.DepartmentID != "" && target.ScopeID == selector.DepartmentID {
				department = &targets[i]
			}
		case ScopeWorkspace:
			workspace = &targets[i]
		}
	}

	for _, candidate := range []*Target{member, department, workspace} {
		if candidate == nil {
			continue
		}
		return Resolution{
			Value:    candidate.Value,
			Currency: candidate.Currency,
			Scope:    candidate.Scope,
			ScopeID:  candidate.ScopeID,
		}, true
	}
	return Resolution{}, false
}

func FilterVisible(targets []Target, departmentScope []string, scoped bool) []Target {
	if !scoped {
		return targets
	}
	allowed := make(map[string]struct{}, len(departmentScope))
	for _, id := range departmentScope {
		if id == "" {
			continue
		}
		allowed[id] = struct{}{}
	}
	out := make([]Target, 0, len(targets))
	for _, target := range targets {
		if target.Scope != ScopeDepartment {
			out = append(out, target)
			continue
		}
		if _, found := allowed[target.ScopeID]; found {
			out = append(out, target)
		}
	}
	return out
}
