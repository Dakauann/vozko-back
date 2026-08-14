package tools_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/actor"
	leadmemory "vozko/domain/lead_memory"
	"vozko/usecases/agentctx"
)

type memToolFakes struct {
	createdWith *leadmemory.CreateInput
	updatedWith *leadmemory.UpdateInput
	deletedWith *leadmemory.DeleteInput

	createResult *leadmemory.CreateResult
	createErr    error
	updateErr    error
	deleteErr    error
}

func (f *memToolFakes) createUC() leadmemory.CreateUseCase { return createFn{f} }
func (f *memToolFakes) updateUC() leadmemory.UpdateUseCase { return updateFn{f} }
func (f *memToolFakes) deleteUC() leadmemory.DeleteUseCase { return deleteFn{f} }

type createFn struct{ f *memToolFakes }
type updateFn struct{ f *memToolFakes }
type deleteFn struct{ f *memToolFakes }

func (c createFn) Execute(_ context.Context, in leadmemory.CreateInput) (*leadmemory.CreateResult, error) {
	c.f.createdWith = &in
	if c.f.createErr != nil {
		return nil, c.f.createErr
	}
	if c.f.createResult != nil {
		return c.f.createResult, nil
	}
	return &leadmemory.CreateResult{Memory: &leadmemory.LeadMemory{ID: "11111111-2222-4333-8444-555555555555"}}, nil
}

func (u updateFn) Execute(_ context.Context, in leadmemory.UpdateInput) (*leadmemory.LeadMemory, error) {
	u.f.updatedWith = &in
	if u.f.updateErr != nil {
		return nil, u.f.updateErr
	}
	return &leadmemory.LeadMemory{ID: "11111111-2222-4333-8444-555555555555"}, nil
}

func (d deleteFn) Execute(_ context.Context, in leadmemory.DeleteInput) error {
	d.f.deletedWith = &in
	return d.f.deleteErr
}

func memToolConfig() map[string]interface{} {
	return map[string]interface{}{
		"__workspace_id": "ws-1",
		"__lead_id":      "lead-1",
		"__agent_id":     "agent-1",
		"__entry_id":     "entry-1",
		"__entry_type":   "whatsapp",
	}
}

func newMemTool(f *memToolFakes) *manageLeadMemoryTool {
	return NewManageLeadMemoryToolUseCase(f.createUC(), f.updateUC(), f.deleteUC()).(*manageLeadMemoryTool)
}

func TestMemoryToolRememberFlowsIdentityFromConfig(t *testing.T) {
	f := &memToolFakes{}
	tool := newMemTool(f)

	res, err := tool.ExecuteWithConfig(context.Background(), memToolConfig(), map[string]interface{}{
		"action":   "remember",
		"content":  "Prefere boleto.",
		"category": "preference",
	})
	if err != nil || res.IsError {
		t.Fatalf("remember = (%+v, %v)", res, err)
	}
	in := f.createdWith
	if in == nil || in.WorkspaceID != "ws-1" || in.LeadID != "lead-1" {
		t.Fatalf("identity not taken from config: %+v", in)
	}
	// Attribution is the seeded agent, formatted as an AI actor.
	if in.Actor.Kind != actor.KindAI || in.Actor.ID != "ai:agent-1" {
		t.Fatalf("actor = %+v", in.Actor)
	}
	if in.SourceEntryID == nil || *in.SourceEntryID != "entry-1" || *in.SourceEntryType != "whatsapp" {
		t.Fatalf("provenance not seeded: %+v", in)
	}
	if !strings.Contains(res.Result.(string), "[11111111]") {
		t.Fatalf("result should carry the short id: %v", res.Result)
	}
}

func TestMemoryToolWithoutLeadIsGraceful(t *testing.T) {
	f := &memToolFakes{}
	tool := newMemTool(f)

	cfg := memToolConfig()
	delete(cfg, "__lead_id")
	res, err := tool.ExecuteWithConfig(context.Background(), cfg, map[string]interface{}{
		"action": "remember", "content": "x",
	})
	// A conversation not bridged to a lead answers with a tool RESULT the model
	// can act on, never a Go error, which would abort the turn.
	if err != nil {
		t.Fatalf("must not return a Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Result.(string), "não está vinculada a um lead") {
		t.Fatalf("expected graceful no-lead result, got %+v", res)
	}
	if f.createdWith != nil {
		t.Fatal("nothing may be written without a lead")
	}
}

func TestMemoryToolMapsDomainErrorsToGuidance(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"limit", leadmemory.ErrLimitReached, "Limite de memórias"},
		{"ambiguous", leadmemory.ErrAmbiguousID, "mais de uma memória"},
		{"not found", leadmemory.ErrNotFound, "não encontrada"},
		{"too long", leadmemory.ErrContentTooLong, "excede o limite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &memToolFakes{createErr: tc.err, updateErr: tc.err}
			tool := newMemTool(f)
			res, err := tool.ExecuteWithConfig(context.Background(), memToolConfig(), map[string]interface{}{
				"action": "update", "memory_id": "11111111", "content": "x",
			})
			if err != nil || !res.IsError {
				t.Fatalf("expected error result, got (%+v, %v)", res, err)
			}
			if !strings.Contains(res.Result.(string), tc.want) {
				t.Fatalf("result %q missing %q", res.Result, tc.want)
			}
		})
	}
}

func TestMemoryToolForgetRequiresID(t *testing.T) {
	f := &memToolFakes{}
	tool := newMemTool(f)
	res, _ := tool.ExecuteWithConfig(context.Background(), memToolConfig(), map[string]interface{}{"action": "forget"})
	if !res.IsError || f.deletedWith != nil {
		t.Fatalf("forget without id must refuse, got %+v", res)
	}
}

func TestMemoryToolTrackerDebouncesIdenticalCalls(t *testing.T) {
	f := &memToolFakes{}
	tool := newMemTool(f)
	ctx := agentctx.WithToolExecutionTracker(context.Background(), agentctx.NewToolExecutionTracker())

	params := map[string]interface{}{"action": "remember", "content": "Prefere boleto.", "category": "preference"}
	if res, _ := tool.ExecuteWithConfig(ctx, memToolConfig(), params); res.IsError {
		t.Fatalf("first call failed: %+v", res)
	}
	f.createdWith = nil

	res, _ := tool.ExecuteWithConfig(ctx, memToolConfig(), params)
	// The looping-model case: same call again in the same turn is answered
	// without a second write.
	if f.createdWith != nil {
		t.Fatal("identical repeat within the turn reached storage")
	}
	if res.IsError || !strings.Contains(res.Result.(string), "já foi realizada") {
		t.Fatalf("expected debounce answer, got %+v", res)
	}

	// A DIFFERENT memory in the same turn must still go through.
	other := map[string]interface{}{"action": "remember", "content": "Esposa se chama Ana.", "category": "personal"}
	if res, _ := tool.ExecuteWithConfig(ctx, memToolConfig(), other); res.IsError || f.createdWith == nil {
		t.Fatalf("different write was wrongly debounced: %+v", res)
	}
}

func TestMemoryToolActorFallsBackToSystem(t *testing.T) {
	f := &memToolFakes{}
	tool := newMemTool(f)
	cfg := memToolConfig()
	delete(cfg, "__agent_id")

	if _, err := tool.ExecuteWithConfig(context.Background(), cfg, map[string]interface{}{
		"action": "remember", "content": "x",
	}); err != nil {
		t.Fatal(err)
	}
	if f.createdWith.Actor.Kind != actor.KindSystem || f.createdWith.Actor.ID != actor.SystemID {
		t.Fatalf("expected honest system fallback, got %+v", f.createdWith.Actor)
	}
}
