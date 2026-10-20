package workflow

import (
	"context"
	"errors"
	"testing"
)

func versionedGraph(label string) Graph {
	return Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{"inputs": []interface{}{map[string]interface{}{"name": label}}}},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{{Source: "t", Target: "end"}},
	}
}

func TestGraphHashIsStableAcrossMapOrderAndChangesWithContent(t *testing.T) {
	a := Graph{Nodes: []Node{{ID: "n", Type: NodeTypeEnd, Config: map[string]interface{}{"a": 1.0, "b": "x"}}}}
	b := Graph{Nodes: []Node{{ID: "n", Type: NodeTypeEnd, Config: map[string]interface{}{"b": "x", "a": 1.0}}}}
	c := Graph{Nodes: []Node{{ID: "n", Type: NodeTypeEnd, Config: map[string]interface{}{"a": 2.0, "b": "x"}}}}
	if GraphHash(&a) == "" || GraphHash(&a) != GraphHash(&b) {
		t.Fatal("the same graph must hash the same whatever the map order, or every read would show unpublished changes")
	}
	if GraphHash(&a) == GraphHash(&c) {
		t.Fatal("a changed config must change the hash, or an edit would look published")
	}
}

func TestUnpublishedChangesCompareTheDraftWithThePublishedHash(t *testing.T) {
	one := 1
	draft := versionedGraph("cpf")
	published := GraphHash(&draft)
	cases := []struct {
		name string
		w    Workflow
		want bool
	}{
		{"never published", Workflow{Graph: draft}, true},
		{"draft equals the published version", Workflow{Graph: draft, PublishedVersion: &one, PublishedGraphHash: published}, false},
		{"draft edited after publishing", Workflow{Graph: versionedGraph("rg"), PublishedVersion: &one, PublishedGraphHash: published}, true},
		{"published hash unreadable", Workflow{Graph: draft, PublishedVersion: &one}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.w
			w.RefreshPublication()
			if w.HasUnpublishedChanges != tc.want {
				t.Fatalf("HasUnpublishedChanges = %v, want %v", w.HasUnpublishedChanges, tc.want)
			}
		})
	}
}

func TestCloneDoesNotShareConfigWithTheOriginal(t *testing.T) {
	g := versionedGraph("cpf")
	clone := g.Clone()
	clone.Nodes[0].Config["inputs"].([]interface{})[0].(map[string]interface{})["name"] = "changed"
	clone.Edges[0].Target = "x"
	if g.Nodes[0].Config["inputs"].([]interface{})[0].(map[string]interface{})["name"] != "cpf" || g.Edges[0].Target != "end" {
		t.Fatal("a cached version graph must not change when a caller edits its copy")
	}
}

type versionStore struct {
	workflows map[string]*Workflow
	versions  map[int]*WorkflowVersion
	findErr   error
	lookups   []int
}

func (s *versionStore) FindByID(id string) (*Workflow, error) {
	w, ok := s.workflows[id]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (s *versionStore) FindVersion(_ string, version int) (*WorkflowVersion, error) {
	s.lookups = append(s.lookups, version)
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.versions[version], nil
}

func publishedStore(status WorkflowStatus, published *int) *versionStore {
	draft := versionedGraph("draft")
	v1, v2 := versionedGraph("v1"), versionedGraph("v2")
	return &versionStore{
		workflows: map[string]*Workflow{"wf": {ID: "wf", Status: status, Type: WorkflowTypeMessages, Graph: draft, PublishedVersion: published}},
		versions: map[int]*WorkflowVersion{
			1: {WorkflowID: "wf", Version: 1, Type: WorkflowTypeMessages, Graph: &v1},
			2: {WorkflowID: "wf", Version: 2, Type: WorkflowTypeMessages, Graph: &v2},
		},
	}
}

func inputName(w *Workflow) string {
	return w.Graph.Nodes[0].Config["inputs"].([]interface{})[0].(map[string]interface{})["name"].(string)
}

func TestLoadPublishedRunsThePublishedGraphNotTheDraft(t *testing.T) {
	two := 2
	w, err := LoadPublished(publishedStore(WorkflowStatusActive, &two), "wf")
	if err != nil {
		t.Fatalf("LoadPublished: %v", err)
	}
	if inputName(w) != "v2" || w.GraphVersion == nil || *w.GraphVersion != 2 {
		t.Fatalf("graph = %s version = %v, want the published v2", inputName(w), w.GraphVersion)
	}
}

func TestLoadPublishedFailsClosedWhenAnActiveWorkflowHasNoVersion(t *testing.T) {
	if _, err := LoadPublished(publishedStore(WorkflowStatusActive, nil), "wf"); !errors.Is(err, ErrWorkflowNotPublished) || !errors.Is(err, ErrWorkflowNotActive) {
		t.Fatalf("err = %v, want ErrWorkflowNotPublished (an ErrWorkflowNotActive)", err)
	}
}

func TestLoadPublishedLeavesANeverPublishedDraftForTheStatusCheck(t *testing.T) {
	w, err := LoadPublished(publishedStore(WorkflowStatusDraft, nil), "wf")
	if err != nil || w.Status != WorkflowStatusDraft || w.GraphVersion != nil {
		t.Fatalf("w = %+v err = %v, want the draft row so callers refuse it as not active", w, err)
	}
}

func TestLoadPublishedRefusesAMissingVersionRow(t *testing.T) {
	three := 3
	if _, err := LoadPublished(publishedStore(WorkflowStatusActive, &three), "wf"); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("err = %v, want ErrVersionNotFound", err)
	}
}

func TestPublishedOfHasOneRuleForEveryCaller(t *testing.T) {
	two, three := 2, 3
	store := publishedStore(WorkflowStatusActive, &two)
	w, _ := store.FindByID("wf")
	running, err := PublishedOf(store, w)
	if err != nil || inputName(running) != "v2" || *running.GraphVersion != 2 {
		t.Fatalf("running = %+v err = %v", running, err)
	}

	never, _ := publishedStore(WorkflowStatusDraft, nil).FindByID("wf")
	if running, err := PublishedOf(store, never); running != nil || err != nil {
		t.Fatalf("a never published workflow has nothing published: %+v %v", running, err)
	}

	pruned, _ := publishedStore(WorkflowStatusActive, &three).FindByID("wf")
	if _, err := PublishedOf(store, pruned); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("err = %v, a published version without its row is ErrVersionNotFound, never the draft and never nothing", err)
	}

	store.findErr = errors.New("db down")
	if _, err := PublishedOf(store, w); err == nil || errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("err = %v, a read failure is not a missing version", err)
	}

	if _, err := FindVersionOf(publishedStore(WorkflowStatusActive, &two), "wf", 9); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("FindVersionOf missing row = %v", err)
	}
}

func TestLoadForRunUsesThePinnedVersion(t *testing.T) {
	one, two := 1, 2
	cases := []struct {
		name      string
		published *int
		pinned    *int
		want      string
	}{
		{"pinned to an older version", &two, &one, "v1"},
		{"in flight before versions existed", &two, nil, "v2"},
		{"in flight on a never published workflow", nil, nil, "draft"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := LoadForRun(publishedStore(WorkflowStatusActive, tc.published), &WorkflowRun{WorkflowID: "wf", WorkflowVersion: tc.pinned})
			if err != nil {
				t.Fatalf("LoadForRun: %v", err)
			}
			if inputName(w) != tc.want {
				t.Fatalf("graph = %s, want %s", inputName(w), tc.want)
			}
		})
	}
}

func TestLoadForRunReportsAnUnreadableVersion(t *testing.T) {
	one := 1
	store := publishedStore(WorkflowStatusActive, &one)
	store.findErr = errors.New("db down")
	if _, err := LoadForRun(store, &WorkflowRun{WorkflowID: "wf", WorkflowVersion: &one}); err == nil {
		t.Fatal("an unreadable version must not fall back to the draft")
	}
}

func TestPublisherTravelsInTheContext(t *testing.T) {
	if got := PublisherFrom(WithPublisher(context.Background(), "u-1")); got != "u-1" {
		t.Fatalf("PublisherFrom = %q", got)
	}
	if got := PublisherFrom(context.Background()); got != "" {
		t.Fatalf("PublisherFrom without a publisher = %q", got)
	}
}

func TestCampaignsValidateAgainstTheCampaignVarsOfThePublishedGraph(t *testing.T) {
	campvar := func(name string) Graph {
		return Graph{Nodes: []Node{{ID: "1", Config: map[string]interface{}{"text": "{{campvars." + name + "}}"}}}}
	}
	w := &Workflow{Graph: campvar("draft_only")}
	if got := w.RequiredCampaignVarsOf(nil); len(got) != 1 || got[0] != "draft_only" {
		t.Fatalf("never published = %v, want the draft vars", got)
	}
	published := campvar("published")
	if got := w.RequiredCampaignVarsOf(&WorkflowVersion{Version: 1, Graph: &published}); len(got) != 1 || got[0] != "published" {
		t.Fatalf("published = %v, want the vars of what runs", got)
	}
}

func TestAnAlreadyLoadedWorkflowResolvesTheGraphARunIsPinnedTo(t *testing.T) {
	one, two := 1, 2
	store := publishedStore(WorkflowStatusActive, &two)
	loaded := store.workflows["wf"]
	w, err := ForRun(store, loaded, &WorkflowRun{WorkflowID: "wf", WorkflowVersion: &one})
	if err != nil || inputName(w) != "v1" {
		t.Fatalf("w = %+v err = %v, want the pinned v1 without reading the workflow again", w, err)
	}
	if inputName(loaded) != "draft" {
		t.Fatal("the loaded workflow must not be changed")
	}
}
