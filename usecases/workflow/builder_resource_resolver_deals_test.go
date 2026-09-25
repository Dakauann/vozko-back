package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/pipeline"
	"vozko/domain/stage"
)

type dealCatalogStub struct{}

func (dealCatalogStub) DealPipelines(workspaceID string) ([]*pipeline.Pipeline, error) {
	if workspaceID != "ws1" {
		return nil, nil
	}
	return []*pipeline.Pipeline{{ID: "pl-sales", WorkspaceID: "ws1", Name: "Vendas"}, {ID: "pl-renew", WorkspaceID: "ws1", Name: "Renovações"}}, nil
}

func (dealCatalogStub) PipelineStages(_, pipelineID string) ([]*stage.Stage, error) {
	return []*stage.Stage{{ID: pipelineID + "-won", PipelineID: pipelineID, Name: "Ganho", IsWon: true}}, nil
}

func TestBuilderFindsTheWorkspaceDealFunnelsAndStages(t *testing.T) {
	r := NewBuilderResourceResolver(BuilderResourceResolverDeps{Deals: dealCatalogStub{}})

	funnels, err := r.Search(context.Background(), "ws1", "opportunity_pipelines", "vendas", 5)
	if err != nil || len(funnels) != 1 || funnels[0].ID != "pl-sales" {
		t.Fatalf("funnels = %+v, %v, want only pl-sales", funnels, err)
	}

	stages, err := r.Search(context.Background(), "ws1", "opportunity_stages", "renova", 5)
	if err != nil || len(stages) != 1 || stages[0].ID != "pl-renew-won" || stages[0].Name != "Renovações · Ganho" {
		t.Fatalf("stages = %+v, %v, want the renewal funnel's won stage", stages, err)
	}

	for _, kind := range []string{"opportunity_pipelines", "opportunity_stages"} {
		if !contains(resourceKinds, kind) {
			t.Fatalf("the builder is not told it can look up %s", kind)
		}
	}
}
