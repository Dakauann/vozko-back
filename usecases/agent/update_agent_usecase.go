package agent_usecase

import (
	"context"

	"vozko/domain/agent"
	mcpdomain "vozko/domain/agent/mcp"
	"vozko/domain/rag"
	"vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type updateAgentUseCase struct {
	repo              agent.Repository
	businessPhoneRepo businessphone.Repository
	toolRegistry      tools.Service
	knowledgeBaseRepo rag.KnowledgeBaseRepository
	mcpCollectionRepo mcpdomain.CollectionRepository
}

func NewUpdateAgentUseCase(
	repo agent.Repository,
	businessPhoneRepo businessphone.Repository,
	toolRegistry tools.Service,
	knowledgeBaseRepo rag.KnowledgeBaseRepository,
	mcpCollectionRepo mcpdomain.CollectionRepository,
) agent.UpdateAgentUseCase {
	return &updateAgentUseCase{
		repo:              repo,
		businessPhoneRepo: businessPhoneRepo,
		toolRegistry:      toolRegistry,
		knowledgeBaseRepo: knowledgeBaseRepo,
		mcpCollectionRepo: mcpCollectionRepo,
	}
}

func (uc *updateAgentUseCase) Execute(ctx context.Context, agentID string, in agent.UpdateAgentInput) (*agent.Agent, error) {
	if agentID == "" {
		return nil, agent.ErrAgentNotFound
	}

	current, err := uc.repo.FindByID(agentID)
	if err != nil {
		return nil, err
	}

	if current == nil {
		return nil, agent.ErrAgentNotFound
	}

	// Snapshot the tools before the merge: a partial update that omits InternalTools
	// keeps the existing ones (the merge no-ops), and they are the resolve fallback.
	existingTools := make([]agent.ToolBinding, len(current.InternalTools))
	copy(existingTools, current.InternalTools)

	current.ApplyUpdate(in)

	current.Normalize()
	if err := current.Validate(); err != nil {
		return nil, err
	}

	if err := validateBusinessPhoneOwnership(uc.businessPhoneRepo, current.WorkspaceID, current.BusinessPhoneID); err != nil {
		return nil, err
	}

	// Validated against the MERGED agent, not the input: an update that omits
	// these keeps whatever was already attached, and re-checking the effective
	// set is what makes the guard hold even if something foreign slipped in
	// before this check existed.
	if err := validateKnowledgeBaseOwnership(ctx, uc.knowledgeBaseRepo, current.WorkspaceID, current.KnowledgeBaseIDs); err != nil {
		return nil, err
	}
	if err := validateMCPCollectionOwnership(ctx, uc.mcpCollectionRepo, current.WorkspaceID, current.MCPCollectionIDs); err != nil {
		return nil, err
	}

	selectedTools, err := resolveInternalToolSelection(uc.toolRegistry, in.InternalTools, existingTools)
	if err != nil {
		return nil, err
	}
	current.InternalTools = selectedTools

	if err := syncAgentTools(uc.toolRegistry, current); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(agentID, current); err != nil {
		return nil, err
	}

	updated, err := uc.repo.FindByID(agentID)
	if err != nil {
		return nil, err
	}

	return updated, nil
}
