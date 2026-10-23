package workflow_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/crmfilter"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func leadTriggeredWorkflow(id, workspaceID string, trigger workflow.NodeType, config map[string]interface{}) *workflow.Workflow {
	return &workflow.Workflow{
		ID: id, WorkspaceID: workspaceID, Name: id, Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{{ID: "t", Type: trigger, Config: config}, {ID: "end", Type: workflow.NodeTypeEnd}},
			Edges: []workflow.Edge{{Source: "t", Target: "end"}},
		},
	}
}

func newLeadStarterFor(t *testing.T, runs workflow.WorkflowRunRepository, launcher RunLauncher, state *fakeSharedState) *LeadRunStarter {
	t.Helper()
	starter, err := NewLeadRunStarter(runs, launcher, state)
	if err != nil {
		t.Fatal(err)
	}
	return starter
}

func TestLeadRunStarterStartsOneRunPerOccurrence(t *testing.T) {
	runs := uniqueRunRepo{NewMockWorkflowRunRepository()}
	launcher := &fakeLauncher{}
	state := &fakeSharedState{allow: true}
	starter := newLeadStarterFor(t, runs, launcher, state)
	w := leadTriggeredWorkflow("wf-1", "ws-1", workflow.NodeTypeTriggerBirthday, nil)
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	event := workflow.BirthdayEvent(workflow.LeadSubject{WorkspaceID: "ws-1", LeadID: "lead-1", LeadName: "Ana"}, day)

	first, err := starter.Start(w, event)
	if err != nil || first.Duplicate || first.RunID == "" {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
	again, err := starter.Start(w, event)
	if err != nil || !again.Duplicate || again.RunID != first.RunID {
		t.Fatalf("again = %+v, err = %v", again, err)
	}
	if len(launcher.launched) != 1 {
		t.Fatalf("launched = %d", len(launcher.launched))
	}
	run := launcher.launched[0]
	if run.EntryID != "lead-1" || run.EntryType != workflow.EntryTypeLead || run.TriggerType != workflow.TriggerBirthday || run.State.GetString(workflow.DataKeyLeadName) != "Ana" {
		t.Fatalf("run = %+v", run)
	}
	if state.incrs != 1 {
		t.Fatalf("a duplicate must not take a concurrency slot: %d", state.incrs)
	}
}

func TestLeadRunStarterRefusesWhatItCannotDeduplicate(t *testing.T) {
	starter := newLeadStarterFor(t, uniqueRunRepo{NewMockWorkflowRunRepository()}, &fakeLauncher{}, &fakeSharedState{allow: true})
	w := leadTriggeredWorkflow("wf-1", "ws-1", workflow.NodeTypeTriggerBirthday, nil)
	cases := []struct {
		name  string
		event workflow.TriggerEvent
		want  error
	}{
		{"conversation trigger", workflow.TriggerEvent{WorkspaceID: "ws-1", EntryID: "e", EntryType: "whatsapp", TriggerType: workflow.TriggerMessageReceived}, ErrNotALeadEvent},
		{"lead trigger on a conversation", workflow.TriggerEvent{WorkspaceID: "ws-1", EntryID: "e", EntryType: "whatsapp", TriggerType: workflow.TriggerBirthday}, ErrNotALeadEvent},
		{"no occurrence facts", workflow.TriggerEvent{WorkspaceID: "ws-1", EntryID: "lead-1", EntryType: workflow.EntryTypeLead, TriggerType: workflow.TriggerBirthday, Data: map[string]interface{}{}}, ErrLeadEventNotUnique},
		{"workflow without the trigger", workflow.WebhookLeadEvent(workflow.LeadSubject{WorkspaceID: "ws-1", LeadID: "lead-1"}, "order_paid/o-1", nil), workflow.ErrTriggerNotInWorkflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := starter.Start(w, tc.event); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestLeadRunStarterAtCapacityLeavesNoRun(t *testing.T) {
	runs := uniqueRunRepo{NewMockWorkflowRunRepository()}
	starter := newLeadStarterFor(t, runs, &fakeLauncher{}, &fakeSharedState{allow: false})
	w := leadTriggeredWorkflow("wf-1", "ws-1", workflow.NodeTypeTriggerBirthday, nil)
	event := workflow.BirthdayEvent(workflow.LeadSubject{WorkspaceID: "ws-1", LeadID: "lead-1"}, time.Now())
	if _, err := starter.Start(w, event); !errors.Is(err, workflow.ErrWorkspaceAtCapacity) {
		t.Fatalf("err = %v", err)
	}
	all, _ := runs.List(workflow.ListRunsInput{})
	if len(all.Items) != 0 {
		t.Fatalf("runs = %d", len(all.Items))
	}
}

type triggerWorkflowsStub struct {
	byWorkspace map[string][]*workflow.Workflow
	workspaces  []string
	err         error
}

func (s triggerWorkflowsStub) FindActiveByTrigger(workspaceID string, t workflow.TriggerType) ([]*workflow.Workflow, error) {
	var out []*workflow.Workflow
	for _, w := range s.byWorkspace[workspaceID] {
		if w.HasTriggerType(t) {
			out = append(out, w)
		}
	}
	return out, s.err
}

func (s triggerWorkflowsStub) WorkspacesWithActiveTrigger(workflow.TriggerType) ([]string, error) {
	return s.workspaces, s.err
}

type birthdayLeadsStub struct {
	leads  map[string][]*lead_domain.Lead
	inputs []lead_domain.ListLeadsInput
}

func (s *birthdayLeadsStub) List(input lead_domain.ListLeadsInput) (*shared.PaginatedResult[*lead_domain.Lead], error) {
	s.inputs = append(s.inputs, input)
	all := s.leads[input.WorkspaceID]
	size := input.Options.Pagination.PageSize
	start := (input.Options.Pagination.Page - 1) * size
	if start >= len(all) {
		return &shared.PaginatedResult[*lead_domain.Lead]{Page: input.Options.Pagination.Page, PageSize: size, TotalItems: int64(len(all))}, nil
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	pages := (len(all) + size - 1) / size
	return &shared.PaginatedResult[*lead_domain.Lead]{Items: all[start:end], Page: input.Options.Pagination.Page, PageSize: size, TotalItems: int64(len(all)), TotalPages: pages}, nil
}

type timezonesStub map[string]string

func (s timezonesStub) Timezone(_ context.Context, workspaceID string) (string, error) {
	zone, ok := s[workspaceID]
	if !ok {
		return "", errors.New("no config")
	}
	return zone, nil
}

type startedLeadEvent struct {
	workflowID string
	event      workflow.TriggerEvent
}

type recordingLeadStarter struct {
	started []startedLeadEvent
	err     error
}

func (r *recordingLeadStarter) Start(w *workflow.Workflow, event workflow.TriggerEvent) (LeadStart, error) {
	if r.err != nil {
		return LeadStart{}, r.err
	}
	r.started = append(r.started, startedLeadEvent{workflowID: w.ID, event: event})
	return LeadStart{RunID: "run-" + w.ID + "-" + event.EntryID}, nil
}

func leadsNamed(n int) []*lead_domain.Lead {
	out := make([]*lead_domain.Lead, n)
	for i := range out {
		out[i] = &lead_domain.Lead{ID: "lead-" + string(rune('a'+i)), Name: "L"}
	}
	return out
}

func TestBirthdaySweepStartsDueWorkflowsForTodaysBirthdays(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 0, 0, 0, time.UTC)
	workflows := triggerWorkflowsStub{
		workspaces: []string{"ws-brt", "ws-early", "ws-broken"},
		byWorkspace: map[string][]*workflow.Workflow{
			"ws-brt": {
				leadTriggeredWorkflow("wf-due", "ws-brt", workflow.NodeTypeTriggerBirthday, map[string]interface{}{workflow.BirthdayHourKey: float64(9)}),
				leadTriggeredWorkflow("wf-later", "ws-brt", workflow.NodeTypeTriggerBirthday, map[string]interface{}{workflow.BirthdayHourKey: float64(18)}),
			},
			"ws-early": {leadTriggeredWorkflow("wf-ahead", "ws-early", workflow.NodeTypeTriggerBirthday, map[string]interface{}{workflow.BirthdayHourKey: float64(0)})},
		},
	}
	leads := &birthdayLeadsStub{leads: map[string][]*lead_domain.Lead{"ws-brt": leadsNamed(3), "ws-early": leadsNamed(1)}}
	starter := &recordingLeadStarter{}
	sweep, err := NewBirthdaySweep(BirthdaySweepDeps{
		Workspaces: workflows, Workflows: workflows, Leads: leads, Starter: starter,
		Timezones: timezonesStub{"ws-brt": "America/Sao_Paulo", "ws-early": "Pacific/Kiritimati"},
		Now:       func() time.Time { return now }, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sweep.Execute(context.Background()); err == nil {
		t.Fatal("a workspace whose time zone cannot be read must be reported")
	}

	if len(starter.started) != 4 {
		t.Fatalf("started = %+v", starter.started)
	}
	for _, s := range starter.started[:3] {
		if s.workflowID != "wf-due" || s.event.TriggerType != workflow.TriggerBirthday || s.event.Data[workflow.DataKeyBirthday] != "2026-10-21" {
			t.Fatalf("start = %+v", s)
		}
	}
	ahead := starter.started[3]
	if ahead.workflowID != "wf-ahead" || ahead.event.Data[workflow.DataKeyBirthday] != "2026-10-22" {
		t.Fatalf("a workspace ahead of UTC uses its own day: %+v", ahead)
	}
	input := leads.inputs[0]
	if input.Today.Format("2006-01-02") != "2026-10-21" || len(input.Filter.Groups) != 1 {
		t.Fatalf("input = %+v", input)
	}
	predicate := input.Filter.Groups[0].Predicates[0]
	if predicate.Field != crmfilter.FieldBirthday || predicate.Values[0] != crmfilter.BirthdayToday {
		t.Fatalf("predicate = %+v", predicate)
	}
}

func TestBirthdaySweepSkipsALeadThatCannotStartAndGoesOn(t *testing.T) {
	workflows := triggerWorkflowsStub{
		workspaces:  []string{"ws-1"},
		byWorkspace: map[string][]*workflow.Workflow{"ws-1": {leadTriggeredWorkflow("wf-1", "ws-1", workflow.NodeTypeTriggerBirthday, map[string]interface{}{workflow.BirthdayHourKey: float64(0)})}},
	}
	starter := &recordingLeadStarter{err: workflow.ErrWorkspaceAtCapacity}
	sweep, err := NewBirthdaySweep(BirthdaySweepDeps{
		Workspaces: workflows, Workflows: workflows, Starter: starter,
		Leads:     &birthdayLeadsStub{leads: map[string][]*lead_domain.Lead{"ws-1": leadsNamed(2)}},
		Timezones: timezonesStub{"ws-1": "UTC"},
		Now:       func() time.Time { return time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sweep.Execute(context.Background()); err != nil {
		t.Fatalf("a busy workspace is retried on the next sweep, not an error: %v", err)
	}
}

func TestBirthdaySweepNeedsEveryDependency(t *testing.T) {
	if _, err := NewBirthdaySweep(BirthdaySweepDeps{}); !errors.Is(err, ErrBirthdaySweepIncomplete) {
		t.Fatalf("err = %v", err)
	}
}
