package agent_usecase

import (
	"context"
	"strings"

	"vozko/domain/agent"
	mcpdomain "vozko/domain/agent/mcp"
	"vozko/domain/rag"
)

// Knowledge bases and MCP collections are attached to an agent by id alone, so
// nothing about the id itself proves the caller may use it. Without these
// checks an id from another workspace is accepted verbatim and that
// workspace's documents start grounding this agent's replies.
//
// They live here, next to validateBusinessPhoneOwnership and for the same
// reason: it is an invariant of the agent, not of one transport, so every
// caller (HTTP, copilot, anything later) is covered by one implementation
// instead of each remembering to check.

// uniqueIDs trims, drops blanks and de-duplicates, so the count comparisons
// below are meaningful and a repeated id is not read as a missing one.
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

// validateKnowledgeBaseOwnership rejects any id that is missing or owned by
// another workspace. A nil repo skips the check so a partially wired container
// keeps working, matching validateBusinessPhoneOwnership.
func validateKnowledgeBaseOwnership(ctx context.Context, repo rag.KnowledgeBaseRepository, workspaceID string, ids []string) error {
	wanted := uniqueIDs(ids)
	if len(wanted) == 0 || repo == nil {
		return nil
	}

	found, err := repo.FindByIDs(ctx, wanted)
	if err != nil {
		return err
	}
	// Not-found and foreign are the SAME answer on purpose: replying "that one
	// exists but is not yours" would turn this into an id oracle for another
	// workspace's knowledge bases.
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

// validateMCPCollectionOwnership is the same guard for MCP collections. The
// repository query is already workspace-scoped, so a foreign id simply does
// not come back and the count comparison catches it.
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
