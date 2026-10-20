package workflow

import wd "vozko/domain/workspace/workspace_department"

type DepartmentRestriction struct {
	Restricted    bool
	DepartmentIDs []string
}

func DepartmentRestrictionOf(f *wd.DepartmentFilter) DepartmentRestriction {
	if f == nil {
		return DepartmentRestriction{Restricted: true}
	}
	ids := f.EffectiveDepartmentIDs()
	return DepartmentRestriction{Restricted: len(ids) > 0 || f.ShouldFilter(), DepartmentIDs: ids}
}

func (r DepartmentRestriction) Nothing() bool {
	return r.Restricted && len(r.DepartmentIDs) == 0
}
