package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/stage"
)

type stageListerStub []*stage.Stage

func (s stageListerStub) ListByWorkspace(string) ([]*stage.Stage, error) { return s, nil }

// The AI builder fills a stage node's picker from real stages of the workspace,
// the same list the editor shows.
func TestBuilderFindsTheWorkspaceStages(t *testing.T) {
	r := NewBuilderResourceResolver(BuilderResourceResolverDeps{Stages: stageListerStub{
		{ID: "st-1", WorkspaceID: "ws1", Name: "Proposta enviada"},
		{ID: "st-2", WorkspaceID: "ws1", Name: "Ganho"},
		{ID: "st-x", WorkspaceID: "ws2", Name: "Proposta de outro"},
	}})

	got, err := r.Search(context.Background(), "ws1", "stages", "proposta", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "st-1" {
		t.Fatalf("found %+v, want only st-1", got)
	}
	if !contains(resourceKinds, "stages") {
		t.Fatal("the builder is not told it can look up stages")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
