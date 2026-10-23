package workflow_repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"
)

func TestTheRunExportPagesTheVisibleRunsOfThePeriodAgainstPostgres(t *testing.T) {
	f := newSQLFixture(t, visibilityTables()...)
	sales, support := uuid.NewString(), uuid.NewString()
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	other := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	from := f.at.Add(-7 * 24 * time.Hour)
	seed := func(workflowID, entryID string, startedAt time.Time) string {
		return f.run(runSeed{workflowID: workflowID, entryID: entryID, status: "completed", startedAt: startedAt})
	}
	newest := seed(wf, f.whatsappEntry(sales), f.at.Add(-time.Hour))
	tieA := seed(wf, f.whatsappEntry(sales), f.at.Add(-2*time.Hour))
	tieB := seed(wf, f.whatsappEntry(sales), f.at.Add(-2*time.Hour))
	voice := f.run(runSeed{workflowID: wf, entryType: workflow.EntryTypeSIPCall, status: "completed", startedAt: f.at.Add(-3 * time.Hour)})
	oldest := seed(wf, f.whatsappEntry(sales), from)
	seed(wf, f.whatsappEntry(support), f.at.Add(-90*time.Minute))
	seed(wf, f.whatsappEntry(sales), from.Add(-time.Second))
	seed(wf, f.whatsappEntry(sales), f.at)
	seed(other, f.whatsappEntry(sales), f.at.Add(-time.Hour))
	f.step(newest, "n1", "executed", f.at.Add(-time.Hour))
	f.step(newest, "n2", "failed", f.at.Add(-time.Hour+time.Second))
	f.step(oldest, "n1", "executed", from)

	filter := workflow.RunExportFilter{
		Runs: workflow.ListRunsInput{WorkspaceID: f.workspace, WorkflowID: wf, Visibility: conversation.VisibilityScope{ReadScope: conversation.ReadScope{DepartmentIDs: []string{sales}, RestrictDepartments: true}}},
		From: from, To: f.at,
	}
	source := NewRunExportSource(f.db)
	ctx := context.Background()

	if n, err := source.CountForExportUpTo(ctx, filter, 100); err != nil || n != 5 {
		t.Fatalf("count = %d err = %v", n, err)
	}
	if n, err := source.CountForExportUpTo(ctx, filter, 3); err != nil || n != 3 {
		t.Fatalf("capped count = %d err = %v", n, err)
	}

	ties := []string{tieA, tieB}
	if ties[0] < ties[1] {
		ties[0], ties[1] = ties[1], ties[0]
	}
	want := []string{newest, ties[0], ties[1], voice, oldest}
	var got []string
	var after *workflow.RunCursor
	for pages := 0; pages < 10; pages++ {
		page, err := source.ExportPage(ctx, filter, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range page {
			got = append(got, run.ID)
		}
		if len(page) < 2 {
			break
		}
		after = workflow.CursorAfter(page[len(page)-1])
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("exported %v\nwant      %v", got, want)
	}

	steps := map[string]int{}
	if err := NewRunStepsReader(f.db).EachRunSteps(ctx, []string{oldest, voice, newest}, func(runID string, logs []*workflow.WorkflowRunLog) error {
		steps[runID] = len(logs)
		if runID == newest && (logs[0].NodeID != "n1" || logs[1].NodeID != "n2") {
			t.Fatalf("steps out of execution order: %+v", logs)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[newest] != 2 || steps[oldest] != 1 {
		t.Fatalf("steps = %v", steps)
	}
}

func TestTheRunExportPageIsServedByTheWorkflowStartedIndexAgainstPostgres(t *testing.T) {
	f := newSQLFixture(t)
	f.exec(`CREATE INDEX idx_wfr_workflow_started ON workflow_runs (workflow_id, started_at DESC)`)
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	for i := 0; i < 200; i++ {
		f.run(runSeed{workflowID: wf, status: "completed", startedAt: f.at.Add(-time.Duration(i) * time.Minute)})
	}
	f.exec(`ANALYZE workflow_runs`)
	f.exec(`SET LOCAL enable_seqscan = off`)
	filter := workflow.RunExportFilter{Runs: workflow.ListRunsInput{WorkspaceID: f.workspace, WorkflowID: wf}, From: f.at.Add(-24 * time.Hour), To: f.at.Add(time.Minute)}
	after := &workflow.RunCursor{StartedAt: f.at.Add(-50 * time.Minute), ID: uuid.NewString()}

	statement := f.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []schema.WorkflowRunSchema
		return exportPageQuery(tx, filter, after, 50).Find(&rows)
	})
	var plan []string
	if err := f.db.Raw("EXPLAIN " + statement).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "idx_wfr_workflow_started") || strings.Contains(text, "Seq Scan") {
		t.Fatalf("the keyset page must walk idx_wfr_workflow_started:\n%s", text)
	}
}
