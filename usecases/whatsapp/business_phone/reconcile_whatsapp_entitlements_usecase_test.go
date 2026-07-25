package businessphone_usecase

import (
	"errors"
	"testing"

	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type recordingHandler struct {
	reduced   []string
	increased []string
	reduceErr map[string]error
}

func (h *recordingHandler) OnEntitlementReduced(ws string, _ workspace_addon.EntitlementKind) error {
	h.reduced = append(h.reduced, ws)
	if h.reduceErr != nil {
		return h.reduceErr[ws]
	}
	return nil
}
func (h *recordingHandler) OnEntitlementIncreased(ws string, _ workspace_addon.EntitlementKind) error {
	h.increased = append(h.increased, ws)
	return nil
}

type fakeBatchResolver struct {
	limits map[string]int
	err    error
	calls  int
}

func (f *fakeBatchResolver) ResolveMany(ids []string, _ workspace_addon.EntitlementKind) (map[string]int, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]int, len(ids))
	for _, id := range ids {
		out[id] = f.limits[id]
	}
	return out, nil
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestReconcile_SuspendsOnlyOverCapWorkspaces(t *testing.T) {
	reader := &fakeOwnerReader{connectedCnt: map[string]int{"wsOver": 2, "wsOk": 1}}
	resolver := &fakeBatchResolver{limits: map[string]int{"wsOver": 1, "wsOk": 1}}
	handler := &recordingHandler{}
	uc := NewReconcileWhatsAppEntitlementsUseCase(reader, resolver, handler)

	if _, err := uc.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !contains(handler.reduced, "wsOver") {
		t.Fatalf("expected over-cap wsOver suspended, reduced=%v", handler.reduced)
	}
	if contains(handler.reduced, "wsOk") {
		t.Fatalf("at-cap wsOk must not be touched, reduced=%v", handler.reduced)
	}
	if len(handler.increased) != 0 {
		t.Fatalf("no workspace had suspended numbers, increased=%v", handler.increased)
	}
}

func TestReconcile_ReactivatesUnderServedWorkspaceWithSuspended(t *testing.T) {
	reader := &fakeOwnerReader{
		connectedCnt: map[string]int{}, // wsUnder has 0 connected
		suspendedWS:  []string{"wsUnder"},
	}
	resolver := &fakeBatchResolver{limits: map[string]int{"wsUnder": 1}} // room: 1 > 0
	handler := &recordingHandler{}
	uc := NewReconcileWhatsAppEntitlementsUseCase(reader, resolver, handler)

	if _, err := uc.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !contains(handler.increased, "wsUnder") {
		t.Fatalf("expected wsUnder reactivated, increased=%v", handler.increased)
	}
	if len(handler.reduced) != 0 {
		t.Fatalf("nothing was over cap, reduced=%v", handler.reduced)
	}
}

func TestReconcile_NoDriftNoHandlerCalls(t *testing.T) {
	reader := &fakeOwnerReader{connectedCnt: map[string]int{"wsA": 1, "wsB": 3}}
	resolver := &fakeBatchResolver{limits: map[string]int{"wsA": 1, "wsB": 3}}
	handler := &recordingHandler{}
	uc := NewReconcileWhatsAppEntitlementsUseCase(reader, resolver, handler)

	n, err := uc.Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 0 || len(handler.reduced) != 0 || len(handler.increased) != 0 {
		t.Fatalf("steady state must touch nothing, n=%d reduced=%v increased=%v", n, handler.reduced, handler.increased)
	}
}

func TestReconcile_ResolveErrorSkipsChunkFailSafe(t *testing.T) {
	reader := &fakeOwnerReader{connectedCnt: map[string]int{"wsOver": 5}}
	resolver := &fakeBatchResolver{err: errors.New("db down")}
	handler := &recordingHandler{}
	uc := NewReconcileWhatsAppEntitlementsUseCase(reader, resolver, handler)

	n, err := uc.Execute()
	if err != nil {
		t.Fatalf("a resolve failure must be a per-chunk skip, not a hard error: %v", err)
	}
	// Never suspend on uncertain data.
	if n != 0 || len(handler.reduced) != 0 {
		t.Fatalf("must not act when entitlement could not be resolved, n=%d reduced=%v", n, handler.reduced)
	}
}

func TestReconcile_PhoneReaderErrorPropagates(t *testing.T) {
	reader := &fakeOwnerReader{listErr: errors.New("db down")}
	resolver := &fakeBatchResolver{}
	handler := &recordingHandler{}
	uc := NewReconcileWhatsAppEntitlementsUseCase(reader, resolver, handler)

	if _, err := uc.Execute(); err == nil {
		t.Fatal("expected the phone reader error to propagate")
	}
	if resolver.calls != 0 {
		t.Fatalf("must not resolve when enumeration failed, calls=%d", resolver.calls)
	}
}
