package workflow_usecase

import (
	"context"
	"log"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

type ModelCatalog interface {
	GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error)
}

func checkGraphReasoning(ctx context.Context, catalog ModelCatalog, g *workflow.Graph) error {
	if !workflow.GraphChoosesReasoning(g) {
		return nil
	}
	var models []ai.ModelInfo
	if catalog != nil {
		var err error
		if models, err = catalog.GetModelsWithPricing(ctx); err != nil {
			log.Printf("[workflow] model catalog unavailable, refusing the chosen reasoning levels: %v", err)
		}
	}
	return workflow.CheckGraphReasoning(g, models)
}
