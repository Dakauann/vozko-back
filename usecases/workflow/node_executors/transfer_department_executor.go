package node_executors

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workflow"
	dept "vozko/domain/workspace/workspace_department"
)

var departmentRoundRobin = struct {
	sync.Mutex
	idx map[string]int
}{idx: make(map[string]int)}

type transferDepartmentExecutor struct {
	deptRepo       dept.Repository
	assignmentRepo ia.Repository
}

func NewTransferDepartmentExecutor(deptRepo dept.Repository, assignmentRepo ia.Repository) workflow.NodeExecutor {
	return &transferDepartmentExecutor{deptRepo: deptRepo, assignmentRepo: assignmentRepo}
}

func (e *transferDepartmentExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionTransferDepartment,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Transferir para Departamento",
		Description: "Transfere a conversa para um membro do departamento selecionado usando distribuição round-robin.",
		Icon:        "UsersThree",
		Guidance: workflow.NodeGuidance{
			When:     "Para transferir a conversa a um departamento (distribuição round-robin entre membros).",
			Behavior: "Distribui a conversa entre os membros do departamento por rodízio (round-robin); normalmente encerra a automação.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a transferência é realizada com sucesso"},
			{Key: "department_id", Description: "ID do departamento"},
			{Key: "department_name", Description: "Nome do departamento"},
			{Key: "assigned_user_id", Description: "ID do usuário atribuído"},
			{Key: "assigned_user_email", Description: "E-mail do usuário atribuído"},
			{Key: "error", Description: "Descrição do erro quando a transferência falha"},
		},
		DefaultConfig: map[string]interface{}{
			"department_id": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "department_id", Label: "Departamento", Type: "select", OptionsSource: "departments", Required: true},
		},
	}
}

func (e *transferDepartmentExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	departmentID, _ := ctx.Node.Config["department_id"].(string)
	if strings.TrimSpace(departmentID) == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	departmentID = workflow.Interpolate(departmentID, ctx.State, nil)

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	// Defense-in-depth: in contexts that don't wire department infrastructure
	// (e.g. the workflow simulator), the repos are nil. Fail gracefully via the
	// "erro" handle instead of dereferencing a nil interface (which would panic).
	if e.deptRepo == nil || e.assignmentRepo == nil {
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   "transferência de departamento indisponível neste contexto (ex.: simulação)",
			},
		}, nil
	}

	department, err := e.deptRepo.GetDepartmentByID(departmentID)
	if err != nil || department == nil {
		errMsg := fmt.Sprintf("departamento %q não encontrado", departmentID)
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   errMsg,
			},
		}, nil
	}

	members, err := e.deptRepo.ListMembers(departmentID)
	if err != nil {
		errMsg := fmt.Sprintf("erro ao listar membros do departamento: %v", err)
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   errMsg,
			},
		}, nil
	}
	if len(members) == 0 {
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   "departamento não possui membros",
			},
		}, nil
	}

	sort.Slice(members, func(i, j int) bool {
		return members[i].UserID < members[j].UserID
	})

	departmentRoundRobin.Lock()
	lastIdx := departmentRoundRobin.idx[departmentID]
	nextIdx := lastIdx % len(members)
	departmentRoundRobin.idx[departmentID] = nextIdx + 1
	departmentRoundRobin.Unlock()

	chosen := members[nextIdx]

	assignment := &ia.InboxAssignment{
		WorkspaceID:    ctx.Run.WorkspaceID,
		EntryID:        ctx.Run.EntryID,
		EntryType:      ctx.Run.EntryType,
		AssignedUserID: chosen.UserID,
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
			"department_id":       department.ID,
			"department_name":     department.Name,
			"assigned_user_id":    chosen.UserID,
			"assigned_user_email": chosen.Email,
		},
	}, nil
}
