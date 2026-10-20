package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
)

func TestTheCatalogIsBuiltOnceAndRebuiltOnlyWhenADefinitionIsAdded(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	registry.RegisterDefinition(workflow.NodeDefinition{Type: workflow.NodeTypeEnd})

	first := registry.Catalog()
	if allocs := testing.AllocsPerRun(100, func() { _ = registry.Catalog() }); allocs != 0 {
		t.Fatalf("Catalog allocates %v times per call, want a cached answer", allocs)
	}

	registry.RegisterDefinition(workflow.NodeDefinition{Type: workflow.NodeTypeTriggerManual})
	if len(first) != 1 || len(registry.Catalog()) != 2 {
		t.Fatalf("catalog = %d then %d definitions", len(first), len(registry.Catalog()))
	}
}
