package http

import (
	"testing"

	"github.com/gorilla/mux"

	wsdelivery "vozko/delivery/ws"
	"vozko/domain/metrics"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"
)

type noopWSMetrics struct{}

func (noopWSMetrics) IncWSConnections(string) {}
func (noopWSMetrics) DecWSConnections(string) {}

// Verify the AI builder WebSocket routes are registered (edit + create) when the
// handler is wired. The NewRouter positional wiring is already type-checked by
// the compiler (the handler type differs from its neighbors), so this guards the
// route paths/methods specifically.
func TestSetupWorkflowRoutes_RegistersAIBuilderRoutes(t *testing.T) {
	var m metrics.WSMetricsRecorder = noopWSMetrics{}
	uc := workflow_usecase.NewAIBuilderUseCase(workflow_usecase.AIBuilderUseCaseDeps{})
	handler := wsdelivery.NewWSWorkflowAIBuilderHandler(uc, m, nil)
	if handler == nil {
		t.Fatal("handler should be non-nil")
	}

	r := &router{
		workspaceMiddleware:        middleware.NewWorkspaceMiddleware(nil, nil, nil),
		wsWorkflowAIBuilderHandler: handler,
	}

	mr := mux.NewRouter()
	r.setupWorkflowRoutes(mr)

	want := map[string]bool{
		"/ws/workflows/{id}/ai-builder": false,
		"/ws/workflows/ai-builder":      false,
	}
	_ = mr.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if tpl, err := route.GetPathTemplate(); err == nil {
			if _, ok := want[tpl]; ok {
				want[tpl] = true
			}
		}
		return nil
	})
	for path, found := range want {
		if !found {
			t.Errorf("route %q was not registered", path)
		}
	}
}

func TestSetupWorkflowRoutes_NoAIBuilderRouteWhenHandlerNil(t *testing.T) {
	r := &router{workspaceMiddleware: middleware.NewWorkspaceMiddleware(nil, nil, nil)}
	mr := mux.NewRouter()
	r.setupWorkflowRoutes(mr)
	_ = mr.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if tpl, err := route.GetPathTemplate(); err == nil && tpl == "/ws/workflows/ai-builder" {
			t.Errorf("ai-builder route must not register when handler is nil")
		}
		return nil
	})
}
