package agent_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/agent"
	mcpdomain "vozko/domain/agent/mcp"
	"vozko/domain/rag"
	"vozko/domain/shared"
)

type fakeKBRepo struct {
	byID map[string]*rag.KnowledgeBase
}

func (f fakeKBRepo) FindByIDs(_ context.Context, ids []string) ([]*rag.KnowledgeBase, error) {
	out := make([]*rag.KnowledgeBase, 0, len(ids))
	for _, id := range ids {
		if kb, ok := f.byID[id]; ok {
			out = append(out, kb)
		}
	}
	return out, nil
}
func (f fakeKBRepo) Create(context.Context, *rag.KnowledgeBase) error { return nil }
func (f fakeKBRepo) Update(context.Context, *rag.KnowledgeBase) error { return nil }
func (f fakeKBRepo) Delete(context.Context, string) error             { return nil }
func (f fakeKBRepo) FindByID(context.Context, string) (*rag.KnowledgeBase, error) {
	return nil, nil
}
func (f fakeKBRepo) FindByWorkspace(context.Context, string, shared.Pagination) (*shared.PaginatedResult[*rag.KnowledgeBase], error) {
	return nil, nil
}
func (f fakeKBRepo) FindByWorkspaceAndDepartment(context.Context, string, *string, shared.Pagination) (*shared.PaginatedResult[*rag.KnowledgeBase], error) {
	return nil, nil
}
func (f fakeKBRepo) CountByWorkspace(context.Context, string) (int, error)        { return 0, nil }
func (f fakeKBRepo) IncrementDocumentCount(context.Context, string, int) error    { return nil }
func (f fakeKBRepo) IncrementChunkCount(context.Context, string, int) error       { return nil }
func (f fakeKBRepo) UpdateStats(context.Context, string, int, int, float64) error { return nil }

type fakeMCPRepo struct{ byWorkspace map[string]map[string]bool }

func (f fakeMCPRepo) ListByIDs(_ context.Context, ws string, ids []string) ([]*mcpdomain.MCPCollection, error) {
	out := []*mcpdomain.MCPCollection{}
	for _, id := range ids {
		if f.byWorkspace[ws][id] {
			out = append(out, &mcpdomain.MCPCollection{ID: id})
		}
	}
	return out, nil
}
func (f fakeMCPRepo) Create(context.Context, *mcpdomain.MCPCollection) error { return nil }
func (f fakeMCPRepo) Update(context.Context, *mcpdomain.MCPCollection) error { return nil }
func (f fakeMCPRepo) Get(context.Context, string, string) (*mcpdomain.MCPCollection, error) {
	return nil, nil
}
func (f fakeMCPRepo) ListByWorkspace(context.Context, string) ([]*mcpdomain.MCPCollection, error) {
	return nil, nil
}
func (f fakeMCPRepo) Delete(context.Context, string, string) error { return nil }

func kbRepo() fakeKBRepo {
	return fakeKBRepo{byID: map[string]*rag.KnowledgeBase{
		"kb-mine":    {ID: "kb-mine", WorkspaceID: "ws-1"},
		"kb-foreign": {ID: "kb-foreign", WorkspaceID: "ws-2"},
	}}
}

// An id carries no proof of ownership, so attaching another workspace's
// knowledge base must be refused rather than silently grounding this agent in
// someone else's documents.
func TestKnowledgeBaseOwnership(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		ids     []string
		wantErr error
	}{
		"own base":            {[]string{"kb-mine"}, nil},
		"foreign base":        {[]string{"kb-foreign"}, agent.ErrAgentKnowledgeBaseNoAccess},
		"one of each":         {[]string{"kb-mine", "kb-foreign"}, agent.ErrAgentKnowledgeBaseNoAccess},
		"unknown id":          {[]string{"kb-ghost"}, agent.ErrAgentKnowledgeBaseNoAccess},
		"duplicates are fine": {[]string{"kb-mine", "kb-mine"}, nil},
		"none":                {nil, nil},
		"blank only":          {[]string{"  "}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateKnowledgeBaseOwnership(ctx, kbRepo(), "ws-1", tc.ids)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// A nil repo skips the check, matching validateBusinessPhoneOwnership, so
	// a partially wired container still starts.
	if err := validateKnowledgeBaseOwnership(ctx, nil, "ws-1", []string{"kb-foreign"}); err != nil {
		t.Fatalf("nil repo = %v", err)
	}
}

func TestMCPCollectionOwnership(t *testing.T) {
	ctx := context.Background()
	repo := fakeMCPRepo{byWorkspace: map[string]map[string]bool{
		"ws-1": {"mcp-mine": true},
		"ws-2": {"mcp-foreign": true},
	}}

	if err := validateMCPCollectionOwnership(ctx, repo, "ws-1", []string{"mcp-mine"}); err != nil {
		t.Fatalf("own collection = %v", err)
	}
	if err := validateMCPCollectionOwnership(ctx, repo, "ws-1", []string{"mcp-foreign"}); !errors.Is(err, agent.ErrAgentMCPCollectionNoAccess) {
		t.Fatalf("foreign collection = %v", err)
	}
	if err := validateMCPCollectionOwnership(ctx, repo, "ws-1", nil); err != nil {
		t.Fatalf("no ids = %v", err)
	}
}
