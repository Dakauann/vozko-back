package copilottools

import (
	"fmt"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/copilot"
)

var errAgentNotFound = fmt.Errorf("%w: agente não encontrado neste workspace; use o id exato de list_agents", errInvalidArgs)

func ownedAgent(get agent.GetAgentUseCase, cc copilot.Context, raw string) (*agent.Agent, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return nil, fmt.Errorf("%w: id é obrigatório", errInvalidArgs)
	}
	a, err := get.Execute(id)
	if err != nil || a == nil || a.WorkspaceID != cc.WorkspaceID || !cc.Departments.Allows(a.DepartmentID) {
		return nil, errAgentNotFound
	}
	return a, nil
}
