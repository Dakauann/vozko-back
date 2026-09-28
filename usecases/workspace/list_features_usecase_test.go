package workspace_usecase

import (
	"testing"

	"vozko/domain/workspace"
)

func TestListFeaturesReturnsTheCatalogWithoutExposingIt(t *testing.T) {
	features := NewListFeaturesUseCase().Execute()
	if len(features) != len(workspace.Features) {
		t.Fatalf("got %d features, want %d", len(features), len(workspace.Features))
	}
	features[0] = workspace.Feature{}
	if workspace.Features[0].Key == "" {
		t.Fatal("callers must not be able to change the catalog")
	}
}
