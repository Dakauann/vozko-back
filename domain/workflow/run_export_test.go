package workflow

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestARunExportIsTheRunDetailWithItsWorkflowNameAndASchemaVersion(t *testing.T) {
	started := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	completed := started.Add(1500 * time.Millisecond)
	exportedAt := started.Add(time.Hour)
	version := 4
	step := &WorkflowRunLog{ID: "s-1", RunID: "run-1", NodeID: "http", NodeType: "action_http_request", Status: LogStatusFailed, Input: map[string]interface{}{"url": "https://api"}, ErrorKind: "http_status", ErrorChain: []string{"502"}}
	detail := RunDetail{
		Run: &WorkflowRun{
			ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "whatsapp",
			Status: RunStatusCancelled, StartedAt: started, UpdatedAt: completed, CompletedAt: &completed,
			TriggerType: "trigger_message_received", StartedBy: "agent:ag-1", WorkflowVersion: &version,
			State: RunState{Vars: map[string]interface{}{"cpf": "*********01"}}, TriggerData: map[string]interface{}{"message": "oi"},
			Error: "cancelled_by_operator", RetryCount: 1,
		},
		Logs: []*WorkflowRunLog{step},
	}

	doc := ExportOfRun("Matrícula", detail, exportedAt)

	if doc.SchemaVersion != RunExportSchemaVersion || !doc.ExportedAt.Equal(exportedAt) {
		t.Fatalf("header = %d %v", doc.SchemaVersion, doc.ExportedAt)
	}
	run := doc.Run
	if run.ID != "run-1" || run.WorkflowID != "wf-1" || run.WorkflowName != "Matrícula" || run.WorkflowVersion == nil || *run.WorkflowVersion != 4 ||
		run.Status != RunStatusCancelled || run.TriggerType != "trigger_message_received" || run.StartedBy != "agent:ag-1" ||
		run.EntryID != "e-1" || run.Error != "cancelled_by_operator" || run.RetryCount != 1 {
		t.Fatalf("summary = %+v", run)
	}
	if run.DurationMs == nil || *run.DurationMs != 1500 {
		t.Fatalf("a finished run carries its duration, got %v", run.DurationMs)
	}
	if doc.TriggerData["message"] != "oi" || doc.FinalState["cpf"] != "*********01" {
		t.Fatalf("trigger data %v final state %v", doc.TriggerData, doc.FinalState)
	}
	if len(doc.Steps) != 1 || doc.Steps[0] != step {
		t.Fatalf("the steps must be the detail's own records, got %+v", doc.Steps)
	}
}

func TestARunExportOfALiveRunHasNoDurationAndKeepsItsWait(t *testing.T) {
	doc := ExportOfRun("Fluxo", RunDetail{Run: &WorkflowRun{ID: "run-1", Status: RunStatusWaiting, WaitReason: WaitReasonReply, CurrentNodeID: "ask", StartedAt: time.Now()}}, time.Now())
	if doc.Run.DurationMs != nil || doc.Run.WaitReason != WaitReasonReply || doc.Run.CurrentNodeID != "ask" {
		t.Fatalf("summary = %+v", doc.Run)
	}
}

func TestARunExportWithoutStepsWritesAnEmptyListNotNull(t *testing.T) {
	raw, err := json.Marshal(ExportOfRun("Fluxo", RunDetail{Run: &WorkflowRun{ID: "run-1", Status: RunStatusCompleted}}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if steps, ok := decoded["steps"].([]interface{}); !ok || len(steps) != 0 {
		t.Fatalf("steps = %#v", decoded["steps"])
	}
	for _, key := range []string{"schemaVersion", "exportedAt", "run", "triggerData", "finalState"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("the document misses %q: %s", key, raw)
		}
	}
}

func TestARunExportFilterNeedsAPeriodWithinTheCap(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	bogus := RunStatus("failed")
	cases := []struct {
		name   string
		filter RunExportFilter
		want   error
	}{
		{"a full period of 31 days", RunExportFilter{From: from, To: from.Add(MaxRunExportPeriod)}, nil},
		{"one day with a known status", RunExportFilter{From: from, To: from.Add(24 * time.Hour), Runs: ListRunsInput{Status: statusOf(RunStatusError)}}, nil},
		{"no start", RunExportFilter{To: from}, ErrRunExportFilterInvalid},
		{"no end", RunExportFilter{From: from}, ErrRunExportFilterInvalid},
		{"an end before the start", RunExportFilter{From: from, To: from.Add(-time.Hour)}, ErrRunExportFilterInvalid},
		{"an empty period", RunExportFilter{From: from, To: from}, ErrRunExportFilterInvalid},
		{"an unknown status", RunExportFilter{From: from, To: from.Add(time.Hour), Runs: ListRunsInput{Status: &bogus}}, ErrRunExportFilterInvalid},
		{"more than 31 days", RunExportFilter{From: from, To: from.Add(MaxRunExportPeriod + time.Second)}, ErrRunExportTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.filter.Validate(); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func statusOf(s RunStatus) *RunStatus {
	return &s
}
