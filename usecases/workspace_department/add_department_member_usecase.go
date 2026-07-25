package workspace_department_usecase

import (
	"github.com/google/uuid"

	wd "vozko/domain/workspace/workspace_department"
)

type addMemberUseCase struct {
	repo wd.Repository
}

func NewAddMemberUseCase(repo wd.Repository) wd.AddMemberUseCase {
	return &addMemberUseCase{repo: repo}
}

func (uc *addMemberUseCase) Execute(departmentID string, input wd.AddMemberInput) (*wd.DepartmentMember, error) {
	if _, err := uc.repo.GetDepartmentByID(departmentID); err != nil {
		return nil, err
	}

	dm := &wd.DepartmentMember{
		ID:           uuid.New().String(),
		DepartmentID: departmentID,
		MemberID:     input.MemberID,
	}

	if err := uc.repo.AddMember(dm); err != nil {
		return nil, err
	}
	return dm, nil
}
