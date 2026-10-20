package workflow_usecase

import (
	"context"
	"errors"
	"sort"
	"testing"

	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type uniqueRunRepo struct {
	*MockWorkflowRunRepository
}

var errDuplicateRun = errors.New("duplicate key value violates unique constraint")

func (r uniqueRunRepo) Create(run *workflow.WorkflowRun) error {
	if existing, _ := r.FindByID(run.ID); existing != nil {
		return errDuplicateRun
	}
	return r.MockWorkflowRunRepository.Create(run)
}

func formTriggeredWorkflow(id string, config map[string]interface{}) *workflow.Workflow {
	return &workflow.Workflow{
		ID: id, WorkspaceID: "ws-1", Name: id, Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t", Type: workflow.NodeTypeTriggerFormSubmitted, Config: config},
				{ID: "wait", Type: workflow.NodeTypeWaitForReply, Config: map[string]interface{}{"timeout_seconds": float64(3600)}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "t", Target: "wait"},
				{Source: "wait", Target: "end", Label: "replied"},
			},
		},
	}
}

type triggerHarness struct {
	runs  uniqueRunRepo
	firer *FormTriggerFirer
}

func newTriggerHarness(t *testing.T, workflows ...*workflow.Workflow) triggerHarness {
	t.Helper()
	repo := NewMockWorkflowRepository()
	for _, w := range workflows {
		if err := repo.Create(w); err != nil {
			t.Fatal(err)
		}
	}
	runs := uniqueRunRepo{NewMockWorkflowRunRepository()}
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})
	engine := NewRunEngine(runs, NewMockWorkflowRunLogRepository(), registry)
	return triggerHarness{runs: runs, firer: NewFormTriggerFirer(NewTriggerEvaluator(repo, runs, engine, nil))}
}

func (h triggerHarness) submit(t *testing.T, formID, responseID string) {
	t.Helper()
	err := h.firer.FormSubmitted(context.Background(), whatsappform_usecase.FormTrigger{
		WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", FormID: formID, ResponseID: responseID,
		Data: map[string]any{workflow.DataKeyFormResponseID: responseID, "form.email": "ana@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (h triggerHarness) runsByWorkflow() map[string][]*workflow.WorkflowRun {
	all, _ := h.runs.List(workflow.ListRunsInput{})
	out := map[string][]*workflow.WorkflowRun{}
	for _, r := range all.Items {
		out[r.WorkflowID] = append(out[r.WorkflowID], r)
	}
	return out
}

func TestEveryFormResponseStartsItsOwnRunButNeverTwice(t *testing.T) {
	h := newTriggerHarness(t, formTriggeredWorkflow("wf-signup", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}}))

	h.submit(t, "form-1", "resp-1")
	h.submit(t, "form-1", "resp-2")
	h.submit(t, "form-1", "resp-1")

	runs := h.runsByWorkflow()["wf-signup"]
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want one per response", len(runs))
	}
	responses := []string{runs[0].State.GetString(workflow.DataKeyFormResponseID), runs[1].State.GetString(workflow.DataKeyFormResponseID)}
	sort.Strings(responses)
	if responses[0] != "resp-1" || responses[1] != "resp-2" {
		t.Fatalf("responses = %v", responses)
	}
	for _, r := range runs {
		if r.State.GetString(workflow.DataKeyFormID) != "form-1" || r.State.GetString("form.email") != "ana@example.com" || r.Status != workflow.RunStatusWaiting {
			t.Fatalf("run = %+v", r)
		}
	}
}

func TestFormSubmittedStartsOnlyTheWorkflowsForThatForm(t *testing.T) {
	h := newTriggerHarness(t,
		formTriggeredWorkflow("wf-form-1", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}}),
		formTriggeredWorkflow("wf-form-2", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-2"}}),
		formTriggeredWorkflow("wf-any", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{}}),
		formTriggeredWorkflow("wf-unset", nil),
	)

	h.submit(t, "form-1", "resp-1")

	got := h.runsByWorkflow()
	for id, want := range map[string]int{"wf-form-1": 1, "wf-form-2": 0, "wf-any": 1, "wf-unset": 1} {
		if len(got[id]) != want {
			t.Errorf("%s runs = %d, want %d", id, len(got[id]), want)
		}
	}
}

func TestFormSubmittedRefusesAnIncompleteTrigger(t *testing.T) {
	h := newTriggerHarness(t, formTriggeredWorkflow("wf-any", nil))
	complete := whatsappform_usecase.FormTrigger{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", FormID: "form-1", ResponseID: "resp-1"}
	cases := []struct {
		name  string
		shape func(*whatsappform_usecase.FormTrigger)
	}{
		{"no workspace", func(in *whatsappform_usecase.FormTrigger) { in.WorkspaceID = "" }},
		{"no entry", func(in *whatsappform_usecase.FormTrigger) { in.EntryID = "" }},
		{"no entry type", func(in *whatsappform_usecase.FormTrigger) { in.EntryType = "" }},
		{"no form", func(in *whatsappform_usecase.FormTrigger) { in.FormID = "" }},
		{"no response", func(in *whatsappform_usecase.FormTrigger) { in.ResponseID = " " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := complete
			tc.shape(&in)
			if err := h.firer.FormSubmitted(context.Background(), in); !errors.Is(err, ErrFormTriggerIncomplete) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if len(h.runsByWorkflow()) != 0 {
		t.Fatal("an incomplete trigger started a run")
	}
}
