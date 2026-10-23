package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	RunExportSchemaVersion = 1
	MaxRunsPerExport       = 5000
	MaxRunExportPeriod     = 31 * 24 * time.Hour
)

var (
	ErrRunExportFilterInvalid = errors.New("workflow: the run export needs a period (from before to) and a known status")
	ErrRunExportTooLarge      = errors.New("workflow: the run export passes the cap of runs or days")
)

type RunExport struct {
	SchemaVersion int                    `json:"schemaVersion" example:"1"`
	ExportedAt    time.Time              `json:"exportedAt"`
	Run           RunExportSummary       `json:"run"`
	TriggerData   map[string]interface{} `json:"triggerData"`
	FinalState    map[string]interface{} `json:"finalState"`
	Steps         []*WorkflowRunLog      `json:"steps"`
}

type RunExportSummary struct {
	ID              string                 `json:"id"`
	WorkflowID      string                 `json:"workflowId"`
	WorkflowName    string                 `json:"workflowName"`
	WorkflowVersion *int                   `json:"workflowVersion,omitempty"`
	EntryID         string                 `json:"entryId"`
	EntryType       string                 `json:"entryType"`
	Status          RunStatus              `json:"status"`
	TriggerType     TriggerType            `json:"triggerType,omitempty"`
	StartedBy       string                 `json:"startedBy,omitempty"`
	StartedAt       time.Time              `json:"startedAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	CompletedAt     *time.Time             `json:"completedAt,omitempty"`
	DurationMs      *int64                 `json:"durationMs,omitempty"`
	WaitReason      WaitReason             `json:"waitReason,omitempty"`
	CurrentNodeID   string                 `json:"currentNodeId,omitempty"`
	RetryCount      int                    `json:"retryCount"`
	Error           string                 `json:"error,omitempty"`
	AgentResult     map[string]interface{} `json:"agentResult,omitempty"`
}

func ExportOfRun(workflowName string, detail RunDetail, exportedAt time.Time) RunExport {
	run := detail.Run
	if run == nil {
		run = &WorkflowRun{}
	}
	steps := detail.Logs
	if steps == nil {
		steps = []*WorkflowRunLog{}
	}
	return RunExport{
		SchemaVersion: RunExportSchemaVersion,
		ExportedAt:    exportedAt,
		Run: RunExportSummary{
			ID:              run.ID,
			WorkflowID:      run.WorkflowID,
			WorkflowName:    workflowName,
			WorkflowVersion: run.WorkflowVersion,
			EntryID:         run.EntryID,
			EntryType:       run.EntryType,
			Status:          run.Status,
			TriggerType:     run.TriggerType,
			StartedBy:       run.StartedBy,
			StartedAt:       run.StartedAt,
			UpdatedAt:       run.UpdatedAt,
			CompletedAt:     run.CompletedAt,
			DurationMs:      RunDuration(run.Status, run.StartedAt, run.CompletedAt),
			WaitReason:      run.WaitReason,
			CurrentNodeID:   run.CurrentNodeID,
			RetryCount:      run.RetryCount,
			Error:           run.Error,
			AgentResult:     run.AgentResult,
		},
		TriggerData: run.TriggerData,
		FinalState:  run.State.Vars,
		Steps:       steps,
	}
}

func RunDuration(status RunStatus, startedAt time.Time, completedAt *time.Time) *int64 {
	if !status.IsTerminal() || completedAt == nil {
		return nil
	}
	elapsed := completedAt.Sub(startedAt).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return &elapsed
}

type RunExportFilter struct {
	Runs ListRunsInput
	From time.Time
	To   time.Time
}

func (f RunExportFilter) Validate() error {
	if f.From.IsZero() || f.To.IsZero() || !f.To.After(f.From) {
		return fmt.Errorf("%w: from %v, to %v", ErrRunExportFilterInvalid, f.From, f.To)
	}
	if f.Runs.Status != nil && !f.Runs.Status.Valid() {
		return fmt.Errorf("%w: status %q", ErrRunExportFilterInvalid, *f.Runs.Status)
	}
	if f.To.Sub(f.From) > MaxRunExportPeriod {
		return fmt.Errorf("%w: %v", ErrRunExportTooLarge, f.To.Sub(f.From))
	}
	return nil
}

type RunCursor struct {
	StartedAt time.Time
	ID        string
}

func CursorAfter(run *WorkflowRun) *RunCursor {
	return &RunCursor{StartedAt: run.StartedAt, ID: run.ID}
}

type RunExportSource interface {
	CountForExportUpTo(ctx context.Context, filter RunExportFilter, ceiling int) (int64, error)
	ExportPage(ctx context.Context, filter RunExportFilter, after *RunCursor, limit int) ([]*WorkflowRun, error)
}

type RunStepsReader interface {
	EachRunSteps(ctx context.Context, runIDs []string, visit func(runID string, steps []*WorkflowRunLog) error) error
}
