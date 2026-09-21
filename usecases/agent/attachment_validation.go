package agent_usecase

import (
	"context"
	"strings"

	"vozko/domain/agent"
	mcpdomain "vozko/domain/agent/mcp"
	"vozko/domain/rag"
)

func uniqueIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func validateKnowledgeBaseOwnership(ctx context.Context, repo rag.KnowledgeBaseRepository, workspaceID string, ids []string) error {
	wanted := uniqueIDs(ids)
	if len(wanted) == 0 || repo == nil {
		return nil
	}

	found, err := repo.FindByIDs(ctx, wanted)
	if err != nil {
		return err
	}
	owned := 0
	for _, kb := range found {
		if kb == nil || kb.WorkspaceID != workspaceID {
			continue
		}
		owned++
	}
	if owned != len(wanted) {
		return agent.ErrAgentKnowledgeBaseNoAccess
	}
	return nil
}

func validateMCPCollectionOwnership(ctx context.Context, repo mcpdomain.CollectionRepository, workspaceID string, ids []string) error {
	wanted := uniqueIDs(ids)
	if len(wanted) == 0 || repo == nil {
		return nil
	}

	found, err := repo.ListByIDs(ctx, workspaceID, wanted)
	if err != nil {
		return err
	}
	owned := 0
	for _, c := range found {
		if c != nil {
			owned++
		}
	}
	if owned != len(wanted) {
		return agent.ErrAgentMCPCollectionNoAccess
	}
	return nil
}
