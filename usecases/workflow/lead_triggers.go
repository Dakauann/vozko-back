package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var (
	ErrNotALeadEvent            = errors.New("workflow: the event does not start a run on a lead")
	ErrLeadEventNotUnique       = errors.New("workflow: a lead event needs the facts that make it happen only once")
	ErrLeadRunStarterIncomplete = errors.New("workflow: starting runs on leads needs the run repository, the launcher and the shared state")
	ErrBirthdaySweepIncomplete  = errors.New("workflow: the birthday sweep needs the workflows, the leads, the time zones and the starter")
)

const birthdayPageSize = 100

type LeadStart struct {
	RunID     string
	Duplicate bool
}

type LeadRunStarter struct {
	runs        workflow.WorkflowRunRepository
	launcher    RunLauncher
	sharedState cache.SharedState
}

func NewLeadRunStarter(runs workflow.WorkflowRunRepository, launcher RunLauncher, sharedState cache.SharedState) (*LeadRunStarter, error) {
	if runs == nil || launcher == nil || sharedState == nil {
		return nil, ErrLeadRunStarterIncomplete
	}
	return &LeadRunStarter{runs: runs, launcher: launcher, sharedState: sharedState}, nil
}

func (s *LeadRunStarter) Start(w *workflow.Workflow, event workflow.TriggerEvent) (LeadStart, error) {
	if !event.StartsOnLead() || event.EntryID == "" {
		return LeadStart{}, ErrNotALeadEvent
	}
	if event.OccurrenceKey() == "" {
		return LeadStart{}, ErrLeadEventNotUnique
	}
	trigger := w.Graph.TriggerNodeByType(event.TriggerType)
	if trigger == nil {
		return LeadStart{}, fmt.Errorf("%w: %s", workflow.ErrTriggerNotInWorkflow, event.TriggerType)
	}
	run := newTriggeredRun(w, trigger, event)
	existing, err := s.runs.FindByID(run.ID)
	if err != nil {
		return LeadStart{}, err
	}
	if existing != nil {
		return LeadStart{RunID: existing.ID, Duplicate: true}, nil
	}
	if !tryAcquireWorkspaceSlot(s.sharedState, event.WorkspaceID) {
		return LeadStart{}, workflow.ErrWorkspaceAtCapacity
	}
	if err := s.runs.Create(run); err != nil {
		releaseWorkspaceSlot(s.sharedState, event.WorkspaceID)
		return LeadStart{}, err
	}
	slog.Info("workflow.lead_trigger.run_created", append(runLogAttrs(run, run.CurrentNodeID), slog.String("workflow_id", w.ID))...)
	s.launcher.Launch(run, w)
	return LeadStart{RunID: run.ID}, nil
}

type leadStarter interface {
	Start(w *workflow.Workflow, event workflow.TriggerEvent) (LeadStart, error)
}

type ActiveTriggerWorkflows interface {
	FindActiveByTrigger(workspaceID string, triggerType workflow.TriggerType) ([]*workflow.Workflow, error)
}

type TriggerWorkspaces interface {
	WorkspacesWithActiveTrigger(triggerType workflow.TriggerType) ([]string, error)
}

type BirthdayLeads interface {
	List(input lead_domain.ListLeadsInput) (*shared.PaginatedResult[*lead_domain.Lead], error)
}

type WorkspaceTimezones interface {
	Timezone(ctx context.Context, workspaceID string) (string, error)
}

type BirthdaySweepDeps struct {
	Workspaces TriggerWorkspaces
	Workflows  ActiveTriggerWorkflows
	Leads      BirthdayLeads
	Timezones  WorkspaceTimezones
	Starter    leadStarter
	Now        func() time.Time
	PageSize   int
}

type BirthdaySweep struct {
	deps BirthdaySweepDeps
}

func NewBirthdaySweep(deps BirthdaySweepDeps) (*BirthdaySweep, error) {
	if deps.Workspaces == nil || deps.Workflows == nil || deps.Leads == nil || deps.Timezones == nil || deps.Starter == nil {
		return nil, ErrBirthdaySweepIncomplete
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.PageSize <= 0 {
		deps.PageSize = birthdayPageSize
	}
	return &BirthdaySweep{deps: deps}, nil
}

func (s *BirthdaySweep) Execute(ctx context.Context) error {
	workspaces, err := s.deps.Workspaces.WorkspacesWithActiveTrigger(workflow.TriggerBirthday)
	if err != nil {
		return err
	}
	var failures []error
	for _, workspaceID := range workspaces {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.sweepWorkspace(ctx, workspaceID); err != nil {
			failures = append(failures, fmt.Errorf("workspace %s: %w", workspaceID, err))
		}
	}
	return errors.Join(failures...)
}

func (s *BirthdaySweep) sweepWorkspace(ctx context.Context, workspaceID string) error {
	zone, err := s.deps.Timezones.Timezone(ctx, workspaceID)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	localNow := s.deps.Now().In(loc)
	workflows, err := s.deps.Workflows.FindActiveByTrigger(workspaceID, workflow.TriggerBirthday)
	if err != nil {
		return err
	}
	var due []*workflow.Workflow
	for _, w := range workflows {
		if trigger := w.Graph.TriggerNodeByType(workflow.TriggerBirthday); trigger != nil && workflow.BirthdayDue(trigger.Config, localNow) {
			due = append(due, w)
		}
	}
	if len(due) == 0 {
		return nil
	}
	return s.eachBirthdayLead(workspaceID, localNow, func(contact *lead_domain.Lead) {
		event := workflow.BirthdayEvent(workflow.LeadSubject{WorkspaceID: workspaceID, LeadID: contact.ID, LeadName: contact.RealName()}, localNow)
		for _, w := range due {
			if _, err := s.deps.Starter.Start(w, event); err != nil {
				slog.Warn("workflow.birthday.start_skipped", slog.String("workspace_id", workspaceID), slog.String("workflow_id", w.ID),
					slog.String("lead_id", contact.ID), slog.String("error", err.Error()))
			}
		}
	})
}

func (s *BirthdaySweep) eachBirthdayLead(workspaceID string, localNow time.Time, visit func(*lead_domain.Lead)) error {
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: crmfilter.FieldBirthday, Operator: crmfilter.OpEquals, Values: []string{crmfilter.BirthdayToday}}},
	}}}
	for page := 1; ; page++ {
		result, err := s.deps.Leads.List(lead_domain.ListLeadsInput{
			WorkspaceID: workspaceID,
			Filter:      filter,
			Today:       localNow,
			Options: shared.QueryOptions{
				Pagination: shared.Pagination{Page: page, PageSize: s.deps.PageSize},
				Sorts:      []shared.Sort{{Field: string(lead_domain.SortCreatedAt), Direction: shared.SortAsc}},
			},
		})
		if err != nil {
			return err
		}
		for _, contact := range result.Items {
			visit(contact)
		}
		if len(result.Items) == 0 || page >= result.TotalPages {
			return nil
		}
	}
}
