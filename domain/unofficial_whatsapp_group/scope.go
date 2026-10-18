package unofficial_whatsapp_group

import uw "vozko/domain/unofficial_whatsapp"

type Scope struct {
	Restrict    bool
	InstanceIDs []string
}

func ScopeOf(departments uw.DepartmentScope, instances []*uw.Instance) Scope {
	if !departments.Restrict {
		return Scope{}
	}
	s := Scope{Restrict: true, InstanceIDs: []string{}}
	for _, instance := range instances {
		if departments.AllowsInstance(instance) {
			s.InstanceIDs = append(s.InstanceIDs, instance.ID)
		}
	}
	return s
}

func (s Scope) Allows(instanceID string) bool {
	if !s.Restrict {
		return true
	}
	for _, id := range s.InstanceIDs {
		if id == instanceID {
			return true
		}
	}
	return false
}

func (s Scope) Empty() bool {
	return s.Restrict && len(s.InstanceIDs) == 0
}
