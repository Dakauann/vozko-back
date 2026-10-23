package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	wf "vozko/domain/whatsappform"
	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type countingFormSender struct {
	sends []whatsappform_usecase.WorkflowSend
}

func (s *countingFormSender) SendFromWorkflow(_ context.Context, in whatsappform_usecase.WorkflowSend) (wf.Send, error) {
	s.sends = append(s.sends, in)
	return wf.Send{ID: fmt.Sprintf("send-%d", len(s.sends)), FormID: in.FormID, WorkflowRunID: in.RunID}, nil
}

func formWorkflow(edges ...workflow.Edge) *workflow.Workflow {
	return &workflow.Workflow{
		ID: "wf-form", WorkspaceID: "ws-1", Name: "Cadastro", Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived},
				{ID: "form", Type: workflow.NodeTypeActionSendForm, Config: map[string]interface{}{
					workflow.FormNodeFormIDKey: "form-1", "body": "Preencha", "cta": "Abrir",
				}},
				{ID: "done", Type: workflow.NodeTypeEnd},
				{ID: "silent", Type: workflow.NodeTypeEnd},
			},
			Edges: append([]workflow.Edge{{Source: "t", Target: "form"}}, edges...),
		},
	}
}

func completedEdge() workflow.Edge {
	return workflow.Edge{Source: "form", Target: "done", Label: workflow.FormHandleCompleted}
}

func noReplyEdge() workflow.Edge {
	return workflow.Edge{Source: "form", Target: "silent", Label: workflow.FormHandleNoReply}
}

type formHarness struct {
	workflows *MockWorkflowRepository
	runs      *MockWorkflowRunRepository
	engine    *RunEngine
	sender    *countingFormSender
}

func newFormHarness(t *testing.T, w *workflow.Workflow) formHarness {
	t.Helper()
	h := formHarness{workflows: NewMockWorkflowRepository(), runs: NewMockWorkflowRunRepository(), sender: &countingFormSender{}}
	if err := h.workflows.Create(w); err != nil {
		t.Fatal(err)
	}
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{Forms: h.sender})
	h.engine = NewRunEngine(h.runs, NewMockWorkflowRunLogRepository(), registry)
	h.engine.SetAutomationGate(SandboxAutomationGate{})
	return h
}

func (h formHarness) startedRun(t *testing.T, w *workflow.Workflow) *workflow.WorkflowRun {
	t.Helper()
	now := time.Now().UTC()
	run := &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: w.ID, WorkspaceID: w.WorkspaceID, EntryID: "entry-1", EntryType: "whatsapp",
		Status: workflow.RunStatusRunning, TriggerNodeID: "t", CurrentNodeID: "t", State: workflow.NewRunState(), StartedAt: now, UpdatedAt: now,
	}
	if err := h.runs.Create(run); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Execute(run, w); err != nil {
		t.Fatal(err)
	}
	parked, _ := h.runs.FindByID(run.ID)
	if parked.Status != workflow.RunStatusWaiting || parked.WaitReason != workflow.WaitReasonForm || parked.CurrentNodeID != "form" {
		t.Fatalf("the run did not park on the form: %+v", parked)
	}
	return parked
}

func formAnswers() map[string]any {
	return map[string]any{workflow.DataKeyFormID: "form-1", workflow.DataKeyFormResponseID: "resp-1", "form.email": "ana@example.com", "form.cpf": "***.***.***-09"}
}

func TestTheFormNodeSendsWithTheRunAndParksOnTheFormWaitReason(t *testing.T) {
	w := formWorkflow(completedEdge())
	h := newFormHarness(t, w)
	h.startedRun(t, w)

	if len(h.sender.sends) != 1 || h.sender.sends[0].RunID != "run-1" || h.sender.sends[0].FormID != "form-1" {
		t.Fatalf("sends = %+v", h.sender.sends)
	}
}

func TestResumeFormAdvancesThroughCompletedWithTheAnswers(t *testing.T) {
	w := formWorkflow(completedEdge(), noReplyEdge())
	h := newFormHarness(t, w)
	h.startedRun(t, w)

	if err := NewFormRunResumer(h.workflows, h.runs, h.engine).ResumeForm(context.Background(), "run-1", formAnswers()); err != nil {
		t.Fatal(err)
	}

	run, _ := h.runs.FindByID("run-1")
	if run.Status != workflow.RunStatusCompleted || run.CurrentNodeID != "done" {
		t.Fatalf("run = %+v", run)
	}
	for key, want := range formAnswers() {
		if got := run.State.GetString(key); got != want {
			t.Errorf("state %s = %q, want %q", key, got, want)
		}
	}
	if run.State.GetString("_wait_outcome") != workflow.FormHandleCompleted || run.State.GetString("_prev_node_id") != "form" {
		t.Fatalf("state = %+v", run.State.Vars)
	}
}

func TestAnOrdinaryReplyNeverAdvancesARunWaitingOnAForm(t *testing.T) {
	w := formWorkflow(completedEdge())
	w.Graph.Nodes[0].Type = workflow.NodeTypeTriggerMessageReceived
	h := newFormHarness(t, w)
	parked := h.startedRun(t, w)

	if err := AdvanceOnReply(parked, w, map[string]interface{}{"message": "oi"}); !errors.Is(err, ErrRunNotAtWaitNode) {
		t.Fatalf("AdvanceOnReply on a form node = %v", err)
	}

	te := NewTriggerEvaluator(h.workflows, h.runs, h.engine, nil)
	te.Evaluate(workflow.TriggerEvent{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", TriggerType: workflow.TriggerMessageReceived, Data: map[string]interface{}{"message": "oi"}})

	run, _ := h.runs.FindByID("run-1")
	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonForm || run.CurrentNodeID != "form" {
		t.Fatalf("a text reply moved the form run: %+v", run)
	}
	if all, _ := h.runs.List(workflow.ListRunsInput{}); all.TotalItems != 1 || len(h.sender.sends) != 1 {
		t.Fatalf("the reply started another run: %d runs, %d sends", all.TotalItems, len(h.sender.sends))
	}
}

func TestResumeFormIgnoresRunsThatAreNoLongerWaitingOnAForm(t *testing.T) {
	cases := []struct {
		name  string
		shape func(*workflow.WorkflowRun)
	}{
		{"timed out and finished", func(r *workflow.WorkflowRun) { r.SetCompleted() }},
		{"cancelled", func(r *workflow.WorkflowRun) { r.SetCancelled() }},
		{"failed", func(r *workflow.WorkflowRun) { r.SetError("boom") }},
		{"waiting for a reply elsewhere", func(r *workflow.WorkflowRun) { r.WaitReason = workflow.WaitReasonReply }},
		{"retrying the send", func(r *workflow.WorkflowRun) { r.WaitReason = workflow.WaitReasonRetry }},
		{"moved past the form", func(r *workflow.WorkflowRun) { r.CurrentNodeID = "silent" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := formWorkflow(completedEdge())
			h := newFormHarness(t, w)
			parked := h.startedRun(t, w)
			tc.shape(parked)
			if err := h.runs.Update(parked); err != nil {
				t.Fatal(err)
			}

			if err := NewFormRunResumer(h.workflows, h.runs, h.engine).ResumeForm(context.Background(), "run-1", formAnswers()); err != nil {
				t.Fatalf("err = %v", err)
			}

			after, _ := h.runs.FindByID("run-1")
			if after.Status != parked.Status || after.CurrentNodeID != parked.CurrentNodeID || after.State.GetString("form.email") != "" {
				t.Fatalf("run changed: %+v", after)
			}
		})
	}

	h := newFormHarness(t, formWorkflow(completedEdge()))
	if err := NewFormRunResumer(h.workflows, h.runs, h.engine).ResumeForm(context.Background(), "run-missing", formAnswers()); err != nil {
		t.Fatalf("a missing run = %v", err)
	}
}

type failingRunReads struct {
	*MockWorkflowRunRepository
	err error
}

func (r failingRunReads) FindByID(string) (*workflow.WorkflowRun, error) { return nil, r.err }

type failingWorkflowReads struct {
	*MockWorkflowRepository
	err error
}

func (r failingWorkflowReads) FindByID(string) (*workflow.Workflow, error) { return nil, r.err }

func TestResumeFormReturnsInfrastructureErrors(t *testing.T) {
	down := errors.New("database down")
	w := formWorkflow(completedEdge())

	h := newFormHarness(t, w)
	h.startedRun(t, w)
	if err := NewFormRunResumer(h.workflows, failingRunReads{h.runs, down}, h.engine).ResumeForm(context.Background(), "run-1", formAnswers()); !errors.Is(err, down) {
		t.Fatalf("run read failure = %v", err)
	}
	if err := NewFormRunResumer(failingWorkflowReads{h.workflows, down}, h.runs, h.engine).ResumeForm(context.Background(), "run-1", formAnswers()); !errors.Is(err, down) {
		t.Fatalf("workflow read failure = %v", err)
	}
	run, _ := h.runs.FindByID("run-1")
	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonForm {
		t.Fatalf("an infrastructure failure moved the run: %+v", run)
	}
}

func TestResumeFormWithoutACompletedOutputFailsTheRun(t *testing.T) {
	w := formWorkflow(noReplyEdge())
	h := newFormHarness(t, w)
	h.startedRun(t, w)

	if err := NewFormRunResumer(h.workflows, h.runs, h.engine).ResumeForm(context.Background(), "run-1", formAnswers()); err != nil {
		t.Fatal(err)
	}
	run, _ := h.runs.FindByID("run-1")
	if run.Status != workflow.RunStatusError {
		t.Fatalf("run = %+v", run)
	}
}

func TestAFormNobodyAnsweredTimesOutToNoReply(t *testing.T) {
	cases := []struct {
		name       string
		edges      []workflow.Edge
		wantStatus workflow.RunStatus
		wantNode   string
	}{
		{"with no_reply wired", []workflow.Edge{completedEdge(), noReplyEdge()}, workflow.RunStatusCompleted, "silent"},
		{"without no_reply", []workflow.Edge{completedEdge()}, workflow.RunStatusCompleted, "form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := formWorkflow(tc.edges...)
			h := newFormHarness(t, w)
			parked := h.startedRun(t, w)

			if err := resumeRunFromCurrent(h.workflows, h.runs, h.engine, parked); err != nil {
				t.Fatal(err)
			}

			run, _ := h.runs.FindByID("run-1")
			if run.Status != tc.wantStatus || run.CurrentNodeID != tc.wantNode || run.State.GetString("_wait_outcome") != workflow.FormHandleNoReply {
				t.Fatalf("run = %+v", run)
			}
			if len(h.sender.sends) != 1 {
				t.Fatalf("the timeout sent the form again: %+v", h.sender.sends)
			}
		})
	}
}

func TestARetryAtTheFormNodeSendsAgainInsteadOfTimingOut(t *testing.T) {
	w := formWorkflow(completedEdge(), noReplyEdge())
	h := newFormHarness(t, w)
	parked := h.startedRun(t, w)
	parked.WaitReason = workflow.WaitReasonRetry

	if err := resumeRunFromCurrent(h.workflows, h.runs, h.engine, parked); err != nil {
		t.Fatal(err)
	}

	run, _ := h.runs.FindByID("run-1")
	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonForm || run.CurrentNodeID != "form" || len(h.sender.sends) != 2 {
		t.Fatalf("run = %+v, sends = %d", run, len(h.sender.sends))
	}
}

func TestAnAnswerOfAnotherFormNeverResumesTheRun(t *testing.T) {
	for name, formID := range map[string]any{"another form": "form-2", "no form": nil} {
		t.Run(name, func(t *testing.T) {
			w := formWorkflow(completedEdge(), noReplyEdge())
			h := newFormHarness(t, w)
			h.startedRun(t, w)
			data := formAnswers()
			if formID == nil {
				delete(data, workflow.DataKeyFormID)
			} else {
				data[workflow.DataKeyFormID] = formID
			}
			if err := NewFormRunResumer(h.workflows, h.runs, h.engine).ResumeForm(context.Background(), "run-1", data); err != nil {
				t.Fatal(err)
			}
			run, _ := h.runs.FindByID("run-1")
			if run.Status != workflow.RunStatusWaiting || run.CurrentNodeID != "form" {
				t.Fatalf("the run must keep waiting for its own form, got %+v", run)
			}
		})
	}
}
