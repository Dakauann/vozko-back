package webchat

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/pipeline"
	wcdomain "vozko/domain/webchat"
)

func widgetService() (*Widgets, *fakeWidgets) {
	repo := &fakeWidgets{byID: map[string]*wcdomain.Widget{}}
	refs := References{
		Agents:      fakeAgents{"mine": {ID: "mine", WorkspaceID: "ws-1"}, "theirs": {ID: "theirs", WorkspaceID: "ws-2"}},
		Workflows:   fakeWorkflows{"theirs": {ID: "theirs", WorkspaceID: "ws-2"}},
		Pipelines:   fakePipelines{"deals": {ID: "deals", WorkspaceID: "ws-1", ObjectType: pipeline.ObjectOpportunity}},
		Departments: fakeDepartments{"theirs": {ID: "theirs", WorkspaceID: "ws-2"}},
	}
	return NewWidgets(repo, refs), repo
}

func strp(s string) *string { return &s }

func baseInput() WidgetInput {
	origins := []string{"https://loja.example.com"}
	return WidgetInput{Name: strp("Loja"), AllowedOrigins: &origins}
}

func TestCreateWidgetIssuesAPublicKeyAndKeepsTheSecretOnlyWhenVerifying(t *testing.T) {
	uc, _ := widgetService()
	w, err := uc.Create(context.Background(), "ws-1", baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if w.PublicKey == "" || w.IdentitySecret != "" {
		t.Fatalf("public key %q secret %q", w.PublicKey, w.IdentitySecret)
	}

	in := baseInput()
	mode := wcdomain.IdentityRequired
	in.IdentityMode = &mode
	verified, err := uc.Create(context.Background(), "ws-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if verified.IdentitySecret == "" {
		t.Fatal("identity verification needs a generated secret")
	}
}

func TestWidgetRefusesReferencesFromAnotherWorkspace(t *testing.T) {
	cases := map[string]func(*WidgetInput){
		"foreign agent":      func(in *WidgetInput) { in.AgentID = strp("theirs") },
		"unknown agent":      func(in *WidgetInput) { in.AgentID = strp("ghost") },
		"foreign workflow":   func(in *WidgetInput) { in.WorkflowID = strp("theirs") },
		"deal funnel":        func(in *WidgetInput) { in.PipelineID = strp("deals") },
		"foreign department": func(in *WidgetInput) { in.DepartmentID = strp("theirs") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			uc, repo := widgetService()
			in := baseInput()
			mutate(&in)
			if _, err := uc.Create(context.Background(), "ws-1", in); !errors.Is(err, wcdomain.ErrReferenceNotInWorkspace) {
				t.Fatalf("Create = %v", err)
			}
			if len(repo.byID) != 0 {
				t.Fatal("nothing may be saved")
			}
		})
	}
}

func TestWidgetAcceptsItsOwnAgent(t *testing.T) {
	uc, _ := widgetService()
	in := baseInput()
	in.AgentID = strp("mine")
	if _, err := uc.Create(context.Background(), "ws-1", in); err != nil {
		t.Fatal(err)
	}
}

func TestWidgetFromAnotherWorkspaceIsNotFound(t *testing.T) {
	uc, _ := widgetService()
	w, _ := uc.Create(context.Background(), "ws-1", baseInput())
	if _, err := uc.Update(context.Background(), "ws-2", w.ID, baseInput()); !errors.Is(err, wcdomain.ErrWidgetNotFound) {
		t.Fatalf("Update from another workspace = %v", err)
	}
}
