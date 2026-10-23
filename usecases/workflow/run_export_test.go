package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/redact"
	"vozko/domain/workflow"
)

type exportWorkflows struct {
	err   error
	asked []string
}

func (w *exportWorkflows) Get(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	w.asked = append(w.asked, workflowID)
	if w.err != nil {
		return nil, w.err
	}
	return &workflow.Workflow{ID: workflowID, WorkspaceID: scope.WorkspaceID, Name: "Matrícula"}, nil
}

type exportSource struct {
	runs     []*workflow.WorkflowRun
	count    int64
	countErr error
	filters  []workflow.RunExportFilter
	ceilings []int
	limits   []int
	afters   []*workflow.RunCursor
	onPage   func(page int)
}

func (s *exportSource) CountForExportUpTo(_ context.Context, filter workflow.RunExportFilter, ceiling int) (int64, error) {
	s.filters = append(s.filters, filter)
	s.ceilings = append(s.ceilings, ceiling)
	if s.countErr != nil {
		return 0, s.countErr
	}
	return min(s.count, int64(ceiling)), nil
}

func (s *exportSource) ExportPage(_ context.Context, filter workflow.RunExportFilter, after *workflow.RunCursor, limit int) ([]*workflow.WorkflowRun, error) {
	s.filters = append(s.filters, filter)
	s.limits = append(s.limits, limit)
	s.afters = append(s.afters, after)
	if s.onPage != nil {
		s.onPage(len(s.limits))
	}
	start := 0
	if after != nil {
		for i, run := range s.runs {
			if run.ID == after.ID {
				start = i + 1
			}
		}
	}
	end := min(start+limit, len(s.runs))
	return s.runs[start:end], nil
}

type exportSteps struct {
	steps map[string][]*workflow.WorkflowRunLog
	calls [][]string
}

func (s *exportSteps) EachRunSteps(_ context.Context, runIDs []string, visit func(string, []*workflow.WorkflowRunLog) error) error {
	s.calls = append(s.calls, runIDs)
	for _, id := range runIDs {
		if steps, ok := s.steps[id]; ok {
			if err := visit(id, steps); err != nil {
				return err
			}
		}
	}
	return nil
}

type recordingSink struct {
	started  []string
	docs     []workflow.RunExport
	flushes  int
	writeErr error
}

func (s *recordingSink) Start(workflowName string) error {
	s.started = append(s.started, workflowName)
	return nil
}

func (s *recordingSink) Write(doc workflow.RunExport) error {
	if len(s.started) == 0 {
		return errors.New("written before start")
	}
	if s.writeErr != nil {
		return s.writeErr
	}
	s.docs = append(s.docs, doc)
	return nil
}

func (s *recordingSink) Flush() error {
	s.flushes++
	return nil
}

var exportedAt = time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)

func exportScope() workflow.Scope {
	return workflow.Scope{WorkspaceID: "ws-1"}
}

func exportPeriod() workflow.RunExportFilter {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return workflow.RunExportFilter{From: from, To: from.Add(7 * 24 * time.Hour)}
}

func newRunExport(detail workflow.GetRunUseCase, workflows RunExportWorkflows, source workflow.RunExportSource, steps workflow.RunStepsReader, visibility conversation.Visibility) *RunExportUseCase {
	uc := NewRunExportUseCase(detail, workflows, source, steps, visibility)
	uc.now = func() time.Time { return exportedAt }
	return uc
}

func TestOneRunExportIsTheRedactedRunDetailWithItsWorkflowName(t *testing.T) {
	runs, logs := runWithCredentialsInState()
	workflows := &exportWorkflows{}
	uc := newRunExport(NewGetRunUseCase(runs, logs, seesEverything{}), workflows, nil, nil, seesEverything{})

	doc, err := uc.Run(viewer, workflow.Scope{WorkspaceID: "ws-owner"}, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Run.WorkflowName != "Matrícula" || doc.Run.ID != "run-1" || !doc.ExportedAt.Equal(exportedAt) || doc.SchemaVersion != workflow.RunExportSchemaVersion {
		t.Fatalf("doc = %+v", doc.Run)
	}
	if doc.FinalState["unifecaf_access_token"] != redact.Mask || doc.FinalState["curso"] != "ADS" || doc.FinalState["cpf"] != "•••••••••25" {
		t.Fatalf("the final state must be the detail's redacted state: %v", doc.FinalState)
	}
	if len(doc.Steps) != 1 || doc.Steps[0].Output["accessToken"] != redact.Mask || strings.Contains(doc.Steps[0].Error, "t-1") {
		t.Fatalf("the steps must be the detail's redacted steps: %+v", doc.Steps)
	}
	if !reflect.DeepEqual(workflows.asked, []string{"wf-1"}) {
		t.Fatalf("the workflow read = %v", workflows.asked)
	}
}

func TestOneRunExportOfAHiddenConversationIsNotFoundBeforeAnythingElseIsRead(t *testing.T) {
	for _, tc := range runVisibilityCases() {
		if tc.visible {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			workflows := &exportWorkflows{}
			uc := newRunExport(NewGetRunUseCase(conversationRun(tc.runEntry), NewMockWorkflowRunLogRepository(), tc.rule), workflows, nil, nil, tc.rule)
			if _, err := uc.Run(viewer, exportScope(), "run-1"); !errors.Is(err, workflow.ErrRunNotFound) {
				t.Fatalf("err = %v", err)
			}
			if len(workflows.asked) != 0 {
				t.Fatal("a hidden run must not reach the workflow read")
			}
		})
	}
}

func TestOneRunExportFollowsTheWorkflowsDepartment(t *testing.T) {
	uc := newRunExport(NewGetRunUseCase(conversationRun("whatsapp"), NewMockWorkflowRunLogRepository(), seesEverything{}), &exportWorkflows{err: workflow.ErrWorkflowAccessDenied}, nil, nil, seesEverything{})
	if _, err := uc.Run(viewer, exportScope(), "run-1"); !errors.Is(err, workflow.ErrWorkflowAccessDenied) {
		t.Fatalf("err = %v", err)
	}
}

func exportedRuns(n int) []*workflow.WorkflowRun {
	start := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	out := make([]*workflow.WorkflowRun, n)
	for i := range out {
		state := workflow.NewRunState()
		state.Set("access_token", fmt.Sprintf("tok-%d", i))
		out[i] = &workflow.WorkflowRun{ID: fmt.Sprintf("run-%03d", i), WorkflowID: "wf-1", WorkspaceID: "ws-1", Status: workflow.RunStatusCompleted, StartedAt: start.Add(-time.Duration(i) * time.Minute), State: state}
	}
	return out
}

func TestManyRunsAreStreamedInBoundedPagesWithTheirStepsReadOncePerPage(t *testing.T) {
	runs := exportedRuns(120)
	source := &exportSource{runs: runs, count: 120}
	steps := &exportSteps{steps: map[string][]*workflow.WorkflowRunLog{
		"run-000": {{ID: "s-1", RunID: "run-000", NodeID: "http", Output: map[string]interface{}{"accessToken": "tok-0"}}},
		"run-075": {{ID: "s-2", RunID: "run-075", NodeID: "n1"}, {ID: "s-3", RunID: "run-075", NodeID: "n2"}},
	}}
	sink := &recordingSink{}
	rule := inboxRule{departments: []string{salesDepartment}}
	filter := exportPeriod()
	status := workflow.RunStatusCompleted
	filter.Runs.Status, filter.Runs.EntryID = &status, "entry-9"

	if err := newRunExport(nil, &exportWorkflows{}, source, steps, rule).Stream(context.Background(), viewer, exportScope(), "wf-1", filter, sink); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(sink.started, []string{"Matrícula"}) || len(sink.docs) != 120 || sink.flushes != 3 {
		t.Fatalf("started %v, %d docs, %d flushes", sink.started, len(sink.docs), sink.flushes)
	}
	if !reflect.DeepEqual(source.limits, []int{runExportPageSize, runExportPageSize, runExportPageSize}) || source.afters[0] != nil || source.afters[1].ID != "run-049" || !source.afters[1].StartedAt.Equal(runs[49].StartedAt) {
		t.Fatalf("pages = %v after %+v", source.limits, source.afters)
	}
	if len(steps.calls) != 3 || len(steps.calls[0]) != runExportPageSize || steps.calls[1][0] != "run-050" {
		t.Fatalf("the steps must be read once per page: %v", len(steps.calls))
	}
	for i, doc := range sink.docs {
		if doc.Run.ID != runs[i].ID || doc.Run.WorkflowName != "Matrícula" || !doc.ExportedAt.Equal(exportedAt) || doc.Steps == nil {
			t.Fatalf("doc %d = %+v", i, doc.Run)
		}
		if doc.FinalState["access_token"] != redact.Mask {
			t.Fatalf("doc %d state not redacted: %v", i, doc.FinalState)
		}
	}
	if len(sink.docs[0].Steps) != 1 || sink.docs[0].Steps[0].Output["accessToken"] != redact.Mask || len(sink.docs[75].Steps) != 2 || len(sink.docs[1].Steps) != 0 {
		t.Fatalf("steps were not attached to their runs or not redacted: %+v", sink.docs[0].Steps)
	}
	for _, used := range source.filters {
		if used.Runs.WorkspaceID != "ws-1" || used.Runs.WorkflowID != "wf-1" || used.Runs.EntryID != "entry-9" || *used.Runs.Status != status || !used.Runs.Visibility.RestrictDepartments || !used.From.Equal(filter.From) {
			t.Fatalf("the filter lost the list rules: %+v", used)
		}
	}
	if source.ceilings[0] != workflow.MaxRunsPerExport+1 {
		t.Fatalf("the count must stop past the cap, ceiling = %d", source.ceilings[0])
	}
	if stored, _ := runs[0].State.Get("access_token"); stored != "tok-0" {
		t.Fatal("the export must never touch the stored run")
	}
}

func TestAnExportPastTheCapIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	source := &exportSource{runs: exportedRuns(3), count: workflow.MaxRunsPerExport + 1}
	sink := &recordingSink{}
	err := newRunExport(nil, &exportWorkflows{}, source, &exportSteps{}, seesEverything{}).Stream(context.Background(), viewer, exportScope(), "wf-1", exportPeriod(), sink)
	if !errors.Is(err, workflow.ErrRunExportTooLarge) || len(sink.started) != 0 || len(source.limits) != 0 {
		t.Fatalf("err = %v started %v pages %v", err, sink.started, source.limits)
	}
}

func TestAnExportWithAnInvalidPeriodIsRefusedBeforeTheCount(t *testing.T) {
	source := &exportSource{}
	err := newRunExport(nil, &exportWorkflows{}, source, &exportSteps{}, seesEverything{}).Stream(context.Background(), viewer, exportScope(), "wf-1", workflow.RunExportFilter{}, &recordingSink{})
	if !errors.Is(err, workflow.ErrRunExportFilterInvalid) || len(source.filters) != 0 {
		t.Fatalf("err = %v, counted %d", err, len(source.filters))
	}
}

func TestAnExportOfAnotherDepartmentsWorkflowIsRefusedBeforeTheCount(t *testing.T) {
	source := &exportSource{}
	err := newRunExport(nil, &exportWorkflows{err: workflow.ErrWorkflowAccessDenied}, source, &exportSteps{}, seesEverything{}).Stream(context.Background(), viewer, exportScope(), "wf-1", exportPeriod(), &recordingSink{})
	if !errors.Is(err, workflow.ErrWorkflowAccessDenied) || len(source.filters) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestAnUnreadableConversationScopeExportsNoConversationRun(t *testing.T) {
	source := &exportSource{}
	if err := newRunExport(nil, &exportWorkflows{}, source, &exportSteps{}, inboxRule{err: errScopeRead}).Stream(context.Background(), viewer, exportScope(), "wf-1", exportPeriod(), &recordingSink{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.filters[0].Runs.Visibility, conversation.NothingVisible()) {
		t.Fatalf("scope = %+v", source.filters[0].Runs.Visibility)
	}
}

func TestAnExportStopsWhenTheClientGoesAway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &exportSource{runs: exportedRuns(120), count: 120}
	source.onPage = func(page int) {
		if page == 1 {
			cancel()
		}
	}
	err := newRunExport(nil, &exportWorkflows{}, source, &exportSteps{}, seesEverything{}).Stream(ctx, viewer, exportScope(), "wf-1", exportPeriod(), &recordingSink{})
	if !errors.Is(err, context.Canceled) || len(source.limits) != 1 {
		t.Fatalf("err = %v pages = %d", err, len(source.limits))
	}
}

func TestAnExportStopsAtTheFirstFailedWrite(t *testing.T) {
	source := &exportSource{runs: exportedRuns(120), count: 120}
	broken := errors.New("broken pipe")
	err := newRunExport(nil, &exportWorkflows{}, source, &exportSteps{}, seesEverything{}).Stream(context.Background(), viewer, exportScope(), "wf-1", exportPeriod(), &recordingSink{writeErr: broken})
	if !errors.Is(err, broken) || len(source.limits) != 1 {
		t.Fatalf("err = %v pages = %d", err, len(source.limits))
	}
}

func TestAnExportNeverWritesMoreThanTheCapEvenWhenRunsArriveDuringIt(t *testing.T) {
	source := &exportSource{runs: exportedRuns(workflow.MaxRunsPerExport + 7), count: workflow.MaxRunsPerExport}
	sink := &recordingSink{}
	if err := newRunExport(nil, &exportWorkflows{}, source, &exportSteps{}, seesEverything{}).Stream(context.Background(), viewer, exportScope(), "wf-1", exportPeriod(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.docs) != workflow.MaxRunsPerExport {
		t.Fatalf("docs = %d", len(sink.docs))
	}
}

func TestStepsReadOutOfTheRunOrderFailClosed(t *testing.T) {
	source := &exportSource{runs: exportedRuns(2), count: 2}
	steps := &reversedSteps{}
	err := newRunExport(nil, &exportWorkflows{}, source, steps, seesEverything{}).Stream(context.Background(), viewer, exportScope(), "wf-1", exportPeriod(), &recordingSink{})
	if !errors.Is(err, ErrRunExportStepsOutOfOrder) {
		t.Fatalf("err = %v", err)
	}
}

type reversedSteps struct{}

func (reversedSteps) EachRunSteps(_ context.Context, runIDs []string, visit func(string, []*workflow.WorkflowRunLog) error) error {
	for i := len(runIDs) - 1; i >= 0; i-- {
		if err := visit(runIDs[i], []*workflow.WorkflowRunLog{{ID: "s-" + runIDs[i]}}); err != nil {
			return err
		}
	}
	return nil
}
