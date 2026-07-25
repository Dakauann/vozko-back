package ws

import (
	"testing"

	"vozko/domain/metrics"
	workflow_usecase "vozko/usecases/workflow"
)

type noopWSMetrics struct{}

func (noopWSMetrics) IncWSConnections(string) {}
func (noopWSMetrics) DecWSConnections(string) {}

func TestNewWSWorkflowAIBuilderHandler_NilGuards(t *testing.T) {
	uc := workflow_usecase.NewAIBuilderUseCase(workflow_usecase.AIBuilderUseCaseDeps{})
	var m metrics.WSMetricsRecorder = noopWSMetrics{}

	if NewWSWorkflowAIBuilderHandler(nil, m, nil) != nil {
		t.Fatal("nil usecase must yield nil handler")
	}
	if NewWSWorkflowAIBuilderHandler(uc, nil, nil) != nil {
		t.Fatal("nil metrics must yield nil handler")
	}
	if NewWSWorkflowAIBuilderHandler(uc, m, nil) == nil {
		t.Fatal("valid deps must yield non-nil handler")
	}
}
