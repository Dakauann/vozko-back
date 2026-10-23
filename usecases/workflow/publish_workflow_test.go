package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/usecases/workflow/node_executors"
)

func manualGraph(endID string) workflow.Graph {
	return workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
			{ID: endID, Type: workflow.NodeTypeEnd, Config: map[string]interface{}{}},
		},
		Edges: []workflow.Edge{{Source: "t", Target: endID}},
	}
}

func brokenGraph() workflow.Graph {
	g := manualGraph("end")
	g.Nodes = append(g.Nodes, workflow.Node{ID: "orphan", Type: workflow.NodeTypeEnd, Config: map[string]interface{}{}})
	return g
}

func storedWorkflow(t *testing.T, repo *MockWorkflowRepository, status workflow.WorkflowStatus, g workflow.Graph) {
	t.Helper()
	w := &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws1", Name: "Fluxo", Status: status, Type: workflow.WorkflowTypeMessages, Graph: g, Version: 1}
	w.Normalize()
	if err := repo.Create(w); err != nil {
		t.Fatal(err)
	}
}

func publisherCtx() context.Context {
	return workflow.WithPublisher(context.Background(), "u-1")
}

func activation(repo *MockWorkflowRepository) *activateWorkflowUseCase {
	uc := NewActivateWorkflowUseCase(repo).(*activateWorkflowUseCase)
	uc.SetCatalogFn(func() []workflow.NodeDefinition {
		return append(workflow.BuiltinDefinitions(), node_executors.NewWaitDurationExecutor().Definition())
	})
	return uc
}

func TestSavingAnActiveWorkflowKeepsThePublishedVersionRunning(t *testing.T) {
	for _, status := range []workflow.WorkflowStatus{workflow.WorkflowStatusActive, workflow.WorkflowStatusPaused} {
		t.Run(string(status), func(t *testing.T) {
			repo := NewMockWorkflowRepository()
			storedWorkflow(t, repo, status, manualGraph("end"))

			saved, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, revisionOf(repo, "wf-1"))
			if err != nil {
				t.Fatal(err)
			}
			if saved.Status != status {
				t.Fatalf("status = %s, want %s: a save must never switch the workflow off", saved.Status, status)
			}
			if saved.PublishedVersion == nil || *saved.PublishedVersion != 1 || saved.Version != 2 {
				t.Fatalf("published = %v revision = %d, want version 1 still published and the draft revision bumped", saved.PublishedVersion, saved.Version)
			}
			if !saved.HasUnpublishedChanges {
				t.Fatal("the edited draft must read as unpublished")
			}
			running, err := workflow.LoadPublished(repo, "wf-1")
			if err != nil || running.Graph.FindNode("end") == nil {
				t.Fatalf("the published graph must still be the one that runs: %v", err)
			}
		})
	}
}

func TestSavingStillValidatesTheDraft(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Graph: manualGraph("end")}, revisionOf(repo, "wf-1")); !errors.Is(err, workflow.ErrNameRequired) {
		t.Fatalf("err = %v, want the save validation to stay", err)
	}
}

func TestPublishingSnapshotsTheDraftAndMakesItRun(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, manualGraph("end"))

	published, err := activation(repo).Publish(publisherCtx(), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != workflow.WorkflowStatusActive || published.PublishedVersion == nil || *published.PublishedVersion != 1 {
		t.Fatalf("status = %s version = %v", published.Status, published.PublishedVersion)
	}
	if published.HasUnpublishedChanges {
		t.Fatal("right after publishing there is nothing left to publish")
	}
	v, _ := repo.FindVersion("wf-1", 1)
	if v == nil || v.PublishedBy != "u-1" || v.GraphHash != workflow.GraphHash(&published.Graph) {
		t.Fatalf("version = %+v, want the snapshot with its publisher and hash", v)
	}
}

func TestPublishingAgainTakesTheNextNumber(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}

	published, err := activation(repo).Publish(publisherCtx(), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if *published.PublishedVersion != 2 || published.HasUnpublishedChanges {
		t.Fatalf("version = %d unpublished = %v", *published.PublishedVersion, published.HasUnpublishedChanges)
	}
	running, _ := workflow.LoadPublished(repo, "wf-1")
	if running.Graph.FindNode("fim") == nil || *running.GraphVersion != 2 {
		t.Fatal("new runs must use the newly published graph")
	}
}

func TestPublishingAnUnchangedDraftAddsNoVersion(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusPaused, manualGraph("end"))

	published, err := activation(repo).Publish(publisherCtx(), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if repo.VersionCount("wf-1") != 1 || published.Status != workflow.WorkflowStatusActive {
		t.Fatalf("versions = %d status = %s, want the same version running again", repo.VersionCount("wf-1"), published.Status)
	}
}

func TestABrokenDraftCannotBePublishedAndThePublishedVersionKeepsRunning(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: brokenGraph()}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatalf("a draft with a graph error can still be saved: %v", err)
	}

	if _, err := activation(repo).Publish(publisherCtx(), "wf-1"); !errors.Is(err, workflow.ErrGraphNodeNoIncoming) {
		t.Fatalf("err = %v, want the lint error to block publishing", err)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.Status != workflow.WorkflowStatusActive || repo.VersionCount("wf-1") != 1 {
		t.Fatalf("status = %s versions = %d, want version 1 still active", stored.Status, repo.VersionCount("wf-1"))
	}
}

func TestAnArchivedWorkflowCannotBePublished(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusArchived, manualGraph("end"))
	if _, err := activation(repo).Publish(publisherCtx(), "wf-1"); !errors.Is(err, workflow.ErrCannotActivate) {
		t.Fatalf("err = %v", err)
	}
}

func TestActivatingANeverPublishedWorkflowPublishesIt(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, manualGraph("end"))

	got, err := activation(repo).Execute(publisherCtx(), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != workflow.WorkflowStatusActive || got.PublishedVersion == nil || repo.VersionCount("wf-1") != 1 {
		t.Fatalf("status = %s version = %v", got.Status, got.PublishedVersion)
	}
}

func TestActivatingAPausedWorkflowResumesThePublishedVersionNotTheDraft(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusPaused, manualGraph("end"))
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: brokenGraph()}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}

	got, err := activation(repo).Execute(publisherCtx(), "wf-1")
	if err != nil {
		t.Fatalf("resuming must check the published graph, not the broken draft: %v", err)
	}
	if got.Status != workflow.WorkflowStatusActive || repo.VersionCount("wf-1") != 1 || !got.HasUnpublishedChanges {
		t.Fatalf("status = %s versions = %d unpublished = %v", got.Status, repo.VersionCount("wf-1"), got.HasUnpublishedChanges)
	}
}

func TestPausingPreservesThePublishedVersion(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	got, err := NewPauseWorkflowUseCase(repo, NewMockWorkflowRunRepository()).Execute("wf-1")
	if err != nil || got.Status != workflow.WorkflowStatusPaused || got.PublishedVersion == nil {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestReadsTellWhetherTheDraftDiffersFromThePublishedVersion(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))

	got, err := NewGetWorkflowUseCase(repo).Execute("wf-1")
	if err != nil || got.HasUnpublishedChanges || got.PublishedAt == nil {
		t.Fatalf("got = %+v err = %v, want the published draft with its date", got, err)
	}
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	page, err := NewListWorkflowsUseCase(repo).Execute(workflow.ListWorkflowsInput{WorkspaceID: "ws1"})
	if err != nil || len(page.Items) != 1 || !page.Items[0].HasUnpublishedChanges {
		t.Fatalf("page = %+v err = %v", page, err)
	}
}

func versionsFixture(t *testing.T) (*MockWorkflowRepository, workflow.ScopedWorkflowsUseCase) {
	t.Helper()
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := activation(repo).Publish(publisherCtx(), "wf-1"); err != nil {
		t.Fatal(err)
	}
	activate := activation(repo)
	return repo, NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Create: NewCreateWorkflowUseCase(repo), Get: NewGetWorkflowUseCase(repo), Update: NewUpdateWorkflowUseCase(repo),
		Delete: NewDeleteWorkflowUseCase(repo, nil), Activate: activate, Publish: activate, Pause: NewPauseWorkflowUseCase(repo, NewMockWorkflowRunRepository()),
		Versions: NewWorkflowVersionsUseCase(repo),
	})
}

func ownerScope(userID string) workflow.Scope {
	return workflow.Scope{WorkspaceID: "ws1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}, Requester: workspace.Requester{UserID: userID}}
}

func TestVersionsAreListedNewestFirstOnlyInsideTheWorkspace(t *testing.T) {
	_, uc := versionsFixture(t)
	page, err := uc.Versions(ownerScope("u-1"), "wf-1", shared.QueryOptions{})
	if err != nil || len(page.Items) != 2 || page.Items[0].Version != 2 || page.Items[1].Version != 1 {
		t.Fatalf("page = %+v err = %v", page, err)
	}
	if _, err := uc.Versions(workflow.Scope{WorkspaceID: "ws2", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}, "wf-1", shared.QueryOptions{}); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("another workspace must not see the versions: %v", err)
	}
}

func TestRestoringAVersionCopiesItIntoTheDraftWithoutPublishing(t *testing.T) {
	repo, uc := versionsFixture(t)
	restored, err := uc.RestoreVersion(context.Background(), ownerScope("u-1"), "wf-1", 1, revisionOf(repo, "wf-1"))
	if err != nil {
		t.Fatal(err)
	}
	if restored.Graph.FindNode("end") == nil || restored.Graph.FindNode("fim") != nil {
		t.Fatal("the draft must now hold version 1")
	}
	if *restored.PublishedVersion != 2 || repo.VersionCount("wf-1") != 2 || !restored.HasUnpublishedChanges || restored.Status != workflow.WorkflowStatusActive {
		t.Fatalf("published = %d versions = %d unpublished = %v status = %s, want version 2 still running", *restored.PublishedVersion, repo.VersionCount("wf-1"), restored.HasUnpublishedChanges, restored.Status)
	}
	if _, err := uc.RestoreVersion(context.Background(), ownerScope("u-1"), "wf-1", 7, revisionOf(repo, "wf-1")); !errors.Is(err, workflow.ErrVersionNotFound) {
		t.Fatalf("err = %v, want ErrVersionNotFound", err)
	}
}

func TestScopedPublishRecordsWhoPublished(t *testing.T) {
	repo, uc := versionsFixture(t)
	if _, err := uc.RestoreVersion(context.Background(), ownerScope("u-1"), "wf-1", 1, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Publish(context.Background(), ownerScope("u-9"), "wf-1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := repo.FindVersion("wf-1", 3); v == nil || v.PublishedBy != "u-9" {
		t.Fatalf("version 3 = %+v, want it published by u-9", v)
	}
}

func TestANewDraftReadsAsUnpublished(t *testing.T) {
	repo := NewMockWorkflowRepository()
	created, err := NewCreateWorkflowUseCase(repo).Execute(context.Background(), &workflow.Workflow{WorkspaceID: "ws1", Name: "Novo", Graph: manualGraph("end")})
	if err != nil {
		t.Fatal(err)
	}
	if !created.HasUnpublishedChanges || created.PublishedVersion != nil || created.Status != workflow.WorkflowStatusDraft {
		t.Fatalf("created = %+v", created)
	}
}

func TestTheDetailNamesTheCampaignVarsOfThePublishedVersionNotTheDraft(t *testing.T) {
	repo, uc := versionsFixture(t)
	draft := manualGraph("fim")
	draft.Nodes[1].Config = map[string]interface{}{"note": "{{campvars.cidade}}"}
	if _, err := uc.Update(context.Background(), ownerScope("u-1"), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: draft}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	detail, err := uc.Detail(ownerScope("u-1"), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.RequiredCampaignVars) != 0 {
		t.Fatalf("vars = %v, a campaign must not ask for a variable only the unpublished draft uses", detail.RequiredCampaignVars)
	}
	if detail.Graph.FindNode("fim").Config["note"] != "{{campvars.cidade}}" {
		t.Fatal("the detail still returns the draft graph for the editor")
	}
	if _, err := uc.Detail(workflow.Scope{WorkspaceID: "ws2", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}, "wf-1"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestPublishingStripsPinsFromTheVersionAndKeepsThemOnTheDraft(t *testing.T) {
	repo := NewMockWorkflowRepository()
	g := manualGraph("end")
	g.Nodes = append(g.Nodes, workflow.Node{ID: "set", Type: workflow.NodeTypeWaitDuration, Config: map[string]interface{}{"seconds": float64(5)},
		Pinned: &workflow.NodePin{Output: map[string]interface{}{"x": "1"}, Source: workflow.PinSourceManual, PinnedAt: pinTimeForTests()}})
	g.Edges = []workflow.Edge{{Source: "t", Target: "set"}, {Source: "set", Target: "end", Label: "completed"}}
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, g)

	published, err := activation(repo).Publish(publisherCtx(), "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	v, _ := repo.FindVersion("wf-1", *published.PublishedVersion)
	if v == nil || v.Graph.HasPins() {
		t.Fatal("a published version must never contain a pin, so production can never read one")
	}
	if published.Graph.FindNode("set").Pinned == nil {
		t.Fatal("the draft keeps its pins for the next test")
	}
	if published.HasUnpublishedChanges {
		t.Fatal("a pin alone is not an unpublished change")
	}
	running, err := workflow.LoadPublished(repo, "wf-1")
	if err != nil || running.Graph.HasPins() {
		t.Fatalf("the graph that runs must have no pins: %v", err)
	}
}
