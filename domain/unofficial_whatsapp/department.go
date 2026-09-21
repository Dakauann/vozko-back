package unofficial_whatsapp

import "errors"

var ErrInstanceOutsideDepartment = errors.New("this number belongs to another department")

type DepartmentScope struct {
	DepartmentIDs []string
	Restrict      bool
}

func Unrestricted() DepartmentScope { return DepartmentScope{} }

func (s DepartmentScope) Allows(instanceDepartmentID *string) bool {
	if !s.Restrict {
		return true
	}
	if instanceDepartmentID == nil || *instanceDepartmentID == "" {
		return false
	}
	for _, id := range s.DepartmentIDs {
		if id == *instanceDepartmentID {
			return true
		}
	}
	return false
}

func (s DepartmentScope) AllowsInstance(instance *Instance) bool {
	if instance == nil {
		return false
	}
	return s.Allows(instance.DepartmentID)
}

func (s DepartmentScope) BlocksEverything() bool {
	return s.Restrict && len(s.DepartmentIDs) == 0
}
