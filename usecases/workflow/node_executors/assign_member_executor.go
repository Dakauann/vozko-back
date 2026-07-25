package node_executors

import (
	"fmt"
	"strings"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

type assignMemberExecutor struct {
	workspaceRepo  workspace.Repository
	assignmentRepo ia.Repository
}

func NewAssignMemberExecutor(workspaceRepo workspace.Repository, assignmentRepo ia.Repository) workflow.NodeExecutor {
	return &assignMemberExecutor{workspaceRepo: workspaceRepo, assignmentRepo: assignmentRepo}
}

func (e *assignMemberExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionAssignMember,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Atribuir ao Membro",
		Description: "Atribui a conversa a um membro específico do workspace.",
		Icon:        "UserCircle",
		Guidance: workflow.NodeGuidance{
			When: "Para atribuir a conversa a um atendente específico.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a atribuição é realizada com sucesso"},
			{Key: "assigned_user_id", Description: "ID do usuário atribuído"},
			{Key: "assigned_user_email", Description: "E-mail do usuário atribuído"},
			{Key: "error", Description: "Descrição do erro quando a atribuição falha"},
		},
		DefaultConfig: map[string]interface{}{
			"member_id": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "member_id", Label: "Membro", Type: "select", OptionsSource: "members", Required: true},
		},
	}
}

func (e *assignMemberExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	memberID, _ := ctx.Node.Config["member_id"].(string)
	if strings.TrimSpace(memberID) == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	memberID = workflow.Interpolate(memberID, ctx.State, nil)

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	member, err := e.workspaceRepo.GetMember(ctx.Run.WorkspaceID, memberID)
	if err != nil || member == nil {
		errMsg := fmt.Sprintf("membro %q não encontrado no workspace", memberID)
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   errMsg,
			},
		}, nil
	}

	assignment := &ia.InboxAssignment{
		WorkspaceID:    ctx.Run.WorkspaceID,
		EntryID:        ctx.Run.EntryID,
		EntryType:      ctx.Run.EntryType,
		AssignedUserID: member.UserID,
	}
	if err := e.assignmentRepo.Assign(assignment); err != nil {
		errMsg := fmt.Sprintf("erro ao atribuir conversa: %v", err)
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   errMsg,
			},
		}, nil
	}

	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "sucesso"),
		Output: map[string]interface{}{
			"success":             true,
			"assigned_user_id":    member.UserID,
			"assigned_user_email": member.Email,
		},
	}, nil
}
