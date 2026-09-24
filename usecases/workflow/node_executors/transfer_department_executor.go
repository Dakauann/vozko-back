package node_executors

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/actor"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
)

// transferDepartmentExecutor deals the conversation through the roulette, the
// same ring the first customer message uses, drawing from the chosen
// department or the conversation's own.
type transferDepartmentExecutor struct {
	deptRepo      dept.Repository
	workspaceRepo workspace.Repository
	handOff       ConversationHandOff
}

func NewTransferDepartmentExecutor(deptRepo dept.Repository, workspaceRepo workspace.Repository, handOff ConversationHandOff) workflow.NodeExecutor {
	return &transferDepartmentExecutor{deptRepo: deptRepo, workspaceRepo: workspaceRepo, handOff: handOff}
}

func (e *transferDepartmentExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionTransferDepartment,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Transferir conversa",
		Description: "Passa a conversa a uma pessoa pela roleta do workspace (o mesmo rodízio da primeira mensagem) e pausa a automação. O departamento é opcional: vazio, usa o da conversa.",
		Icon:        "UsersThree",
		Guidance: workflow.NodeGuidance{
			When:     "Para passar a conversa a uma pessoa pela roleta, do departamento da conversa ou de um departamento escolhido.",
			Behavior: "Usa o modo da roleta do workspace (online ou última vez online) e só entrega a quem pode receber conversas. Sem ninguém disponível, a conversa vai para a fila da equipe (queued = true). Pausa a automação desta conversa.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a conversa foi entregue a alguém ou à fila da equipe"},
			{Key: "queued", Description: "true quando ninguém estava disponível e a conversa foi para a fila da equipe"},
			{Key: "department_id", Description: "ID do departamento escolhido (vazio: departamento da conversa)"},
			{Key: "department_name", Description: "Nome do departamento escolhido"},
			{Key: "assigned_user_id", Description: "ID de quem recebeu a conversa (vazio na fila da equipe)"},
			{Key: "assigned_user_email", Description: "E-mail de quem recebeu a conversa"},
			{Key: "error", Description: "Descrição do erro quando a transferência falha"},
		},
		DefaultConfig: map[string]interface{}{
			"department_id": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{
				Key:           "department_id",
				Label:         "Departamento",
				Type:          "select",
				OptionsSource: "departments",
				Placeholder:   "Departamento da conversa",
				Description:   "Vazio: usa o departamento da própria conversa.",
			},
		},
	}
}

func (e *transferDepartmentExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	departmentID, _ := ctx.Node.Config["department_id"].(string)
	departmentID = strings.TrimSpace(workflow.Interpolate(departmentID, ctx.State, nil))
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	if e.handOff == nil {
		return failTransfer(edges, "transferência indisponível neste contexto (ex.: simulação)"), nil
	}

	owner, err := e.handOff.HandOffToRoulette(ia.RouletteHandOff{
		WorkspaceID:  ctx.Run.WorkspaceID,
		EntryID:      ctx.Run.EntryID,
		EntryType:    ctx.Run.EntryType,
		DepartmentID: departmentID,
		ByActorID:    actor.FormatWorkflow(ctx.Run.WorkflowID),
	})
	switch {
	case errors.Is(err, ia.ErrDepartmentOutOfScope):
		return failTransfer(edges, "departamento não pertence a este workspace"), nil
	case err != nil:
		return failTransfer(edges, fmt.Sprintf("erro ao transferir conversa: %v", err)), nil
	}

	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "sucesso"),
		Output: map[string]interface{}{
			"success":             true,
			"queued":              owner == "",
			"department_id":       departmentID,
			"department_name":     e.departmentName(departmentID),
			"assigned_user_id":    owner,
			"assigned_user_email": e.memberEmail(ctx.Run.WorkspaceID, owner),
		},
	}, nil
}

func (e *transferDepartmentExecutor) departmentName(departmentID string) string {
	if departmentID == "" || e.deptRepo == nil {
		return ""
	}
	d, err := e.deptRepo.GetDepartmentByID(departmentID)
	if err != nil || d == nil {
		return ""
	}
	return d.Name
}

func (e *transferDepartmentExecutor) memberEmail(workspaceID, userID string) string {
	if userID == "" || e.workspaceRepo == nil {
		return ""
	}
	m, err := e.workspaceRepo.GetMember(workspaceID, userID)
	if err != nil || m == nil {
		return ""
	}
	return m.Email
}

func failTransfer(edges []workflow.Edge, msg string) *workflow.NodeResult {
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "erro"),
		Output: map[string]interface{}{
			"success": false,
			"error":   msg,
		},
	}
}
