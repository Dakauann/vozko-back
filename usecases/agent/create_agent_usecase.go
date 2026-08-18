package agent_usecase

import (
	"context"

	"github.com/google/uuid"

	"vozko/domain/agent"
	mcpdomain "vozko/domain/agent/mcp"
	"vozko/domain/rag"
	"vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type createAgentUseCase struct {
	repo               agent.Repository
	businessPhoneRepo  businessphone.Repository
	toolRegistry       tools.Service
	knowledgeBaseRepo  rag.KnowledgeBaseRepository
	mcpCollectionRepo  mcpdomain.CollectionRepository
	departmentResolver workspace_department.CreationDepartmentResolver
}

func NewCreateAgentUseCase(
	repo agent.Repository,
	businessPhoneRepo businessphone.Repository,
	toolRegistry tools.Service,
	knowledgeBaseRepo rag.KnowledgeBaseRepository,
	mcpCollectionRepo mcpdomain.CollectionRepository,
	departmentResolver ...workspace_department.CreationDepartmentResolver,
) agent.CreateAgentUseCase {
	var resolver workspace_department.CreationDepartmentResolver
	if len(departmentResolver) > 0 {
		resolver = departmentResolver[0]
	}
	return &createAgentUseCase{
		repo:               repo,
		businessPhoneRepo:  businessPhoneRepo,
		toolRegistry:       toolRegistry,
		knowledgeBaseRepo:  knowledgeBaseRepo,
		mcpCollectionRepo:  mcpCollectionRepo,
		departmentResolver: resolver,
	}
}

func (uc *createAgentUseCase) Execute(ctx context.Context, in agent.CreateAgentInput) (*agent.Agent, error) {
	a := agent.BuildForCreate(in)

	a.Normalize()
	if err := a.Validate(); err != nil {
		return nil, err
	}

	if uc.departmentResolver != nil {
		departmentID, err := uc.departmentResolver.Resolve(ctx, a.WorkspaceID)
		if err != nil {
			return nil, err
		}
		a.DepartmentID = departmentID
	}

	if a.ID == "" {
		a.ID = uuid.New().String()
	}

	if !a.IsActive {
		a.IsActive = true
	}

	if err := validateBusinessPhoneOwnership(uc.businessPhoneRepo, a.WorkspaceID, a.BusinessPhoneID); err != nil {
		return nil, err
	}
	if err := validateKnowledgeBaseOwnership(ctx, uc.knowledgeBaseRepo, a.WorkspaceID, a.KnowledgeBaseIDs); err != nil {
		return nil, err
	}
	if err := validateMCPCollectionOwnership(ctx, uc.mcpCollectionRepo, a.WorkspaceID, a.MCPCollectionIDs); err != nil {
		return nil, err
	}

	selectedTools, err := resolveInternalToolSelection(uc.toolRegistry, a.InternalTools, nil)
	if err != nil {
		return nil, err
	}
	a.InternalTools = selectedTools

	if err := syncAgentTools(uc.toolRegistry, a); err != nil {
		return nil, err
	}

	if err := uc.repo.Create(a); err != nil {
		return nil, err
	}

	created, err := uc.repo.FindByID(a.ID)
	if err != nil {
		return nil, err
	}

	return created, nil
}
