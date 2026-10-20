package sqlexpr

import (
	"strings"
	"testing"
)

func TestTheRunOriginNamesLeadExitsAndManualStartsAndOtherwiseTheTrigger(t *testing.T) {
	want := "(CASE WHEN r.started_by LIKE 'exit:%' THEN 'exit' WHEN r.started_by LIKE 'manual:%' THEN 'manual' ELSE CAST(r.trigger_type AS text) END)"
	if got := WorkflowRunOrigin("r"); got != want {
		t.Fatalf("WorkflowRunOrigin = %q\nwant              %q", got, want)
	}
	if strings.Contains(WorkflowRunOrigin(""), "?") {
		t.Fatal("GORM binds every ?, the expression must carry none")
	}
}
