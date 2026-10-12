package copilottools

import (
	"errors"

	"vozko/domain/copilot"
	wd "vozko/domain/workspace/workspace_department"
)

var ErrInvalidArgs = errors.New("argumento inválido")

func DepartmentFailure(err error) (copilot.Result, bool) {
	switch {
	case errors.Is(err, wd.ErrDepartmentAccessDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "sem acesso a este departamento; o usuário só vê os próprios departamentos"}, true
	case errors.Is(err, wd.ErrDepartmentRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o usuário pertence a mais de um departamento: pergunte qual (list_departments) e passe department_id"}, true
	}
	return copilot.Result{}, false
}
