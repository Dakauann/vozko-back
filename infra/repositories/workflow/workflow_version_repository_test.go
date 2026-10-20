package workflow_repository

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func publishInput() workflow.PublishVersion {
	g := workflow.Graph{Nodes: []workflow.Node{{ID: "t", Type: workflow.NodeTypeTriggerWebhook}, {ID: "end", Type: workflow.NodeTypeEnd}}, Edges: []workflow.Edge{{Source: "t", Target: "end"}}}
	return workflow.PublishVersion{WorkflowID: "wf-1", Type: workflow.WorkflowTypeMessages, Graph: g, GraphHash: workflow.GraphHash(&g), PublishedBy: "u-1", DraftRevision: 4, Draft: g}
}

func TestTheNextVersionNumberIsReadInsideThePublishTransaction(t *testing.T) {
	if got := strings.Count(nextWorkflowVersionSQL, "?"); got != 1 {
		t.Fatalf("placeholders = %d, want 1", got)
	}
}

func expectPublish(mock sqlmock.Sqlmock, next int, insertErr error) {
	mock.ExpectBegin()
	mock.ExpectQuery(sqlPattern(nextWorkflowVersionSQL)).WithArgs("wf-1").WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(next))
	insert := mock.ExpectExec(`^INSERT INTO "workflow_versions" \("id","workflow_id","version","type","graph","graph_hash","published_at","published_by","created_at"\) VALUES`)
	if insertErr != nil {
		insert.WillReturnError(insertErr)
		mock.ExpectRollback()
		return
	}
	insert.WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE "workflows" SET "published_version"=\$1,"status"=\$2,"updated_at"=\$3 WHERE id = \$4 AND "workflows"."deleted_at" IS NULL$`).
		WithArgs(next, "active", sqlmock.AnyArg(), "wf-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestPublishingSnapshotsTheDraftAndMakesTheWorkflowActiveInOneTransaction(t *testing.T) {
	db, mock := mockDB(t)
	expectPublish(mock, 3, nil)
	mock.ExpectCommit()

	v, err := NewWorkflowRepository(db).PublishVersion(publishInput())
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != 3 || v.WorkflowID != "wf-1" || v.PublishedBy != "u-1" || v.GraphHash == "" {
		t.Fatalf("version = %+v", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAConcurrentPublishThatTookTheNumberIsRetriedOnce(t *testing.T) {
	db, mock := mockDB(t)
	taken := &pgconn.PgError{Code: "23505", ConstraintName: "ux_workflow_versions_workflow_version"}
	expectPublish(mock, 3, taken)
	expectPublish(mock, 4, nil)
	mock.ExpectCommit()

	v, err := NewWorkflowRepository(db).PublishVersion(publishInput())
	if err != nil || v.Version != 4 {
		t.Fatalf("version = %+v err = %v, want the retry to take number 4", v, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestASecondConflictIsReportedInsteadOfLooping(t *testing.T) {
	db, mock := mockDB(t)
	taken := &pgconn.PgError{Code: "23505", ConstraintName: "ux_workflow_versions_workflow_version"}
	expectPublish(mock, 3, taken)
	expectPublish(mock, 3, taken)

	if _, err := NewWorkflowRepository(db).PublishVersion(publishInput()); !errors.Is(err, workflow.ErrVersionNumberTaken) {
		t.Fatalf("err = %v, want ErrVersionNumberTaken", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestANormalisedDraftIsWrittenBackOnlyWhenTheDraftDidNotMoveOn(t *testing.T) {
	db, mock := mockDB(t)
	in := publishInput()
	in.RewriteDraft = true
	expectPublish(mock, 1, nil)
	mock.ExpectExec(`^UPDATE "workflows" SET "graph"=\$1,"type"=\$2,"updated_at"=\$3 WHERE \(id = \$4 AND version = \$5\) AND "workflows"."deleted_at" IS NULL$`).
		WithArgs(sqlmock.AnyArg(), "messages", sqlmock.AnyArg(), "wf-1", 4).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if _, err := NewWorkflowRepository(db).PublishVersion(in); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type jsonWithPin bool

func (want jsonWithPin) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, `"pinned"`) == bool(want)
}

func TestTheWrittenBackDraftKeepsItsPinsWhileTheVersionHasNone(t *testing.T) {
	db, mock := mockDB(t)
	draft := workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerWebhook},
			{ID: "h", Type: workflow.NodeTypeActionHTTPRequest, Pinned: &workflow.NodePin{Output: map[string]interface{}{}, Source: workflow.PinSourceManual, PinnedAt: time.Now().UTC()}},
			{ID: "end", Type: workflow.NodeTypeEnd},
		},
	}
	in := workflow.PublishVersionOf(&workflow.Workflow{ID: "wf-1", Type: workflow.WorkflowTypeMessages, Graph: draft, Version: 4}, "u-1", true)
	mock.ExpectBegin()
	mock.ExpectQuery(sqlPattern(nextWorkflowVersionSQL)).WithArgs("wf-1").WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(1))
	mock.ExpectExec(`^INSERT INTO "workflow_versions"`).
		WithArgs(sqlmock.AnyArg(), "wf-1", 1, "messages", jsonWithPin(false), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE "workflows" SET "published_version"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`^UPDATE "workflows" SET "graph"=\$1`).
		WithArgs(jsonWithPin(true), "messages", sqlmock.AnyArg(), "wf-1", 4).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if _, err := NewWorkflowRepository(db).PublishVersion(in); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublishingAGoneWorkflowRollsBack(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(sqlPattern(nextWorkflowVersionSQL)).WithArgs("wf-1").WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(1))
	mock.ExpectExec(`^INSERT INTO "workflow_versions"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE "workflows" SET "published_version"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	if _, err := NewWorkflowRepository(db).PublishVersion(publishInput()); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v, want ErrWorkflowNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAVersionGraphIsReadOnceThenServedFromMemory(t *testing.T) {
	db, mock := mockDB(t)
	published := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`^SELECT \* FROM "workflow_versions" WHERE workflow_id = \$1 AND version = \$2 LIMIT \$3$`).
		WithArgs("wf-1", 2, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workflow_id", "version", "type", "graph", "graph_hash", "published_at", "published_by", "created_at"}).
			AddRow("v-2", "wf-1", 2, "messages", `{"nodes":[{"id":"t","type":"trigger_webhook","position":{"x":0,"y":0},"config":{"a":"b"}}],"edges":[]}`, "h2", published, "u-1", published))

	repo := NewWorkflowRepository(db)
	first, err := repo.FindVersion("wf-1", 2)
	if err != nil || first == nil || first.Graph == nil || first.Graph.Nodes[0].Config["a"] != "b" {
		t.Fatalf("first = %+v err = %v", first, err)
	}
	first.Graph.Nodes[0].Config["a"] = "changed by a caller"
	second, err := repo.FindVersion("wf-1", 2)
	if err != nil || second.Graph.Nodes[0].Config["a"] != "b" || second.GraphHash != "h2" || second.PublishedBy != "u-1" {
		t.Fatalf("second = %+v err = %v, want an untouched cached copy", second, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAMissingVersionIsNotCached(t *testing.T) {
	db, mock := mockDB(t)
	for i := 0; i < 2; i++ {
		mock.ExpectQuery(`^SELECT \* FROM "workflow_versions" WHERE workflow_id = \$1 AND version = \$2 LIMIT \$3$`).
			WithArgs("wf-1", 9, 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	}
	repo := NewWorkflowRepository(db)
	for i := 0; i < 2; i++ {
		if v, err := repo.FindVersion("wf-1", 9); err != nil || v != nil {
			t.Fatalf("v = %+v err = %v", v, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestVersionsAreListedNewestFirstWithoutTheirGraphs(t *testing.T) {
	db, mock := mockDB(t)
	published := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`^SELECT count\(\*\) FROM "workflow_versions" WHERE workflow_id = \$1$`).WithArgs("wf-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`^SELECT "id","workflow_id","version","type","graph_hash","published_at","published_by","created_at" FROM "workflow_versions" WHERE workflow_id = \$1 ORDER BY version DESC LIMIT \$2$`).
		WithArgs("wf-1", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workflow_id", "version", "type", "graph_hash", "published_at", "published_by", "created_at"}).
			AddRow("v-2", "wf-1", 2, "messages", "h2", published, "u-1", published).
			AddRow("v-1", "wf-1", 1, "messages", "h1", published, nil, published))

	page, err := NewWorkflowRepository(db).ListVersions("wf-1", shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 2 || len(page.Items) != 2 || page.Items[0].Version != 2 || page.Items[0].Graph != nil || page.Items[1].PublishedBy != "" {
		t.Fatalf("page = %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAStatusChangeTouchesOnlyTheStatus(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE "workflows" SET "status"=\$1,"updated_at"=\$2 WHERE id = \$3 AND "workflows"."deleted_at" IS NULL$`).
		WithArgs("paused", sqlmock.AnyArg(), "wf-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewWorkflowRepository(db).UpdateStatus("wf-1", workflow.WorkflowStatusPaused); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSavingTheDraftNeverWritesTheStatusOrTheDepartment(t *testing.T) {
	for _, column := range []string{"status", "department_id", "published_version"} {
		if strings.Contains(saveDraftSQL, column) {
			t.Fatalf("the draft save writes %s", column)
		}
	}
	if got := strings.Count(saveDraftSQL, "?"); got != 9 {
		t.Fatalf("save placeholders = %d, want 9", got)
	}
	if got := strings.Count(draftRevisionSQL, "?"); got != 1 {
		t.Fatalf("revision placeholders = %d, want 1", got)
	}
}

func draftToSave() *workflow.Workflow {
	return &workflow.Workflow{ID: "wf-1", Name: "x", Status: workflow.WorkflowStatusDraft, Type: workflow.WorkflowTypeMessages}
}

func TestSavingTheDraftBumpsTheRevisionOnlyFromTheOneItWasBasedOn(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(saveDraftSQL)).
		WithArgs("x", "", "messages", "", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "wf-1", 4).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))

	revision, err := NewWorkflowRepository(db).SaveDraft(draftToSave(), 4)
	if err != nil || revision != 5 {
		t.Fatalf("SaveDraft = %d, %v, want 5", revision, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestASaveBasedOnAnOlderRevisionIsAConflictNamingTheStoredOne(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(saveDraftSQL)).WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectQuery(sqlPattern(draftRevisionSQL)).WithArgs("wf-1").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(6))

	_, err := NewWorkflowRepository(db).SaveDraft(draftToSave(), 4)
	if current, ok := workflow.CurrentDraftRevisionOf(err); !ok || current != 6 {
		t.Fatalf("err = %v, want a conflict at revision 6", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSavingAGoneWorkflowIsNotFoundRatherThanAConflict(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(saveDraftSQL)).WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectQuery(sqlPattern(draftRevisionSQL)).WillReturnRows(sqlmock.NewRows([]string{"version"}))

	if _, err := NewWorkflowRepository(db).SaveDraft(draftToSave(), 4); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestASaveWithoutARevisionNeverReachesTheDatabase(t *testing.T) {
	db, mock := mockDB(t)
	if _, err := NewWorkflowRepository(db).SaveDraft(draftToSave(), 0); !errors.Is(err, workflow.ErrDraftRevisionRequired) {
		t.Fatalf("err = %v, want the revision required", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAssigningADepartmentTouchesOnlyTheDepartment(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE "workflows" SET "department_id"=\$1,"updated_at"=\$2 WHERE id = \$3 AND "workflows"."deleted_at" IS NULL$`).
		WithArgs("dep-1", sqlmock.AnyArg(), "wf-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewWorkflowRepository(db).UpdateDepartment("wf-1", "dep-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAssigningADepartmentToAGoneWorkflowIsNotFound(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE "workflows" SET "department_id"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := NewWorkflowRepository(db).UpdateDepartment("wf-1", "dep-1"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestAWorkflowReadsItsPublicationWithoutLoadingThePublishedGraph(t *testing.T) {
	if strings.Contains(workflowWithPublicationSelect, "v.graph ") || strings.Contains(workflowWithPublicationSelect, "v.graph,") {
		t.Fatal("reading a workflow must not load the published graph")
	}
	db, mock := mockDB(t)
	published := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`^SELECT workflows\.\*, .* AS published_at, .* AS published_graph_hash FROM "workflows" WHERE workflows\.id = \$1 AND "workflows"\."deleted_at" IS NULL LIMIT \$2$`).
		WithArgs("wf-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "status", "type", "graph", "version", "published_version", "published_at", "published_graph_hash"}).
			AddRow("wf-1", "ws-1", "Fluxo", "active", "messages", `{"nodes":[],"edges":[]}`, 7, 2, published, "h2"))

	w, err := NewWorkflowRepository(db).FindByID("wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if w.PublishedVersion == nil || *w.PublishedVersion != 2 || w.PublishedAt == nil || !w.PublishedAt.Equal(published) || w.PublishedGraphHash != "h2" || w.Version != 7 {
		t.Fatalf("workflow = %+v", w)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func publishedJoinRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "workspace_id", "name", "status", "type", "version", "published_version", "published_type", "published_graph"})
}

func TestActiveWorkflowsRunTheirPublishedGraphReadInOneJoinedQuery(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(publishedWorkflowsSQL(false, true))).
		WithArgs("ws-1").
		WillReturnRows(publishedJoinRows().
			AddRow("wf-1", "ws-1", "Com webhook", "active", "messages", 9, 1, "messages", `{"nodes":[{"id":"t","type":"trigger_webhook"}],"edges":[]}`).
			AddRow("wf-2", "ws-1", "Sem webhook publicado", "active", "messages", 3, 1, "messages", `{"nodes":[{"id":"t","type":"trigger_manual"}],"edges":[]}`).
			AddRow("wf-3", "ws-1", "Nunca publicado", "active", "messages", 1, nil, nil, nil).
			AddRow("wf-4", "ws-1", "Versão podada", "active", "messages", 4, 2, nil, nil))

	got, err := NewWorkflowRepository(db).FindActiveByTrigger("ws-1", workflow.TriggerWebhook)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "wf-1" || got[0].GraphVersion == nil || *got[0].GraphVersion != 1 || got[0].TriggerType != workflow.TriggerWebhook {
		t.Fatalf("active = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("one query for the whole workspace, never one per workflow: %v", err)
	}
}

func TestTheWorkspaceWorkflowsComeWithTheirDraftAndPublishedGraphs(t *testing.T) {
	db, mock := mockDB(t)
	rows := sqlmock.NewRows([]string{"id", "workspace_id", "name", "status", "type", "graph", "version", "published_version", "published_type", "published_graph"}).
		AddRow("wf-1", "ws-1", "Publicado", "active", "messages", `{"nodes":[{"id":"d","type":"trigger_manual"}],"edges":[]}`, 9, 1, "messages", `{"nodes":[{"id":"p","type":"trigger_form_submitted"}],"edges":[]}`).
		AddRow("wf-2", "ws-1", "Rascunho", "draft", "messages", `{"nodes":[{"id":"d","type":"trigger_form_submitted"}],"edges":[]}`, 1, nil, nil, nil).
		AddRow("wf-3", "ws-1", "Podado", "active", "messages", `{"nodes":[],"edges":[]}`, 2, 4, nil, nil)
	mock.ExpectQuery(sqlPattern(publishedWorkflowsSQL(true, false))).WithArgs("ws-1").WillReturnRows(rows)

	got, err := NewWorkflowRepository(db).(workflow.PublishedWorkflows).PublishedInWorkspace("ws-1")
	if err != nil || len(got) != 3 {
		t.Fatalf("got = %+v err = %v", got, err)
	}
	if got[0].Draft.Graph.Nodes[0].ID != "d" || got[0].Published == nil || got[0].Published.Graph.Nodes[0].ID != "p" || *got[0].Published.GraphVersion != 1 {
		t.Fatalf("published = %+v", got[0])
	}
	if got[1].Published != nil || got[1].Err != nil {
		t.Fatalf("a never published draft has nothing published: %+v", got[1])
	}
	if got[2].Published != nil || !errors.Is(got[2].Err, workflow.ErrVersionNotFound) {
		t.Fatalf("a missing version row must be ErrVersionNotFound: %+v", got[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheActiveListFiltersByTheTriggerOfThePublishedGraph(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(`^SELECT workflows\.\*, .* FROM "workflows" WHERE workspace_id = \$1 AND status = \$2 AND "workflows"\."deleted_at" IS NULL ORDER BY created_at DESC$`).
		WithArgs("ws-1", "active").
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "status", "type", "graph", "version", "published_version"}).
			AddRow("wf-draft-only", "ws-1", "Gatilho só no rascunho", "active", "messages", `{"nodes":[{"id":"a","type":"trigger_agent_call"}],"edges":[]}`, 5, 1).
			AddRow("wf-published", "ws-1", "Gatilho publicado", "active", "messages", `{"nodes":[{"id":"m","type":"trigger_manual"}],"edges":[]}`, 5, 1))
	published := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	versionRows := func(id, graph string) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "workflow_id", "version", "type", "graph", "graph_hash", "published_at", "published_by", "created_at"}).
			AddRow("v-"+id, id, 1, "messages", graph, "h", published, nil, published)
	}
	mock.ExpectQuery(`^SELECT \* FROM "workflow_versions"`).WithArgs("wf-draft-only", 1, 1).
		WillReturnRows(versionRows("wf-draft-only", `{"nodes":[{"id":"m","type":"trigger_manual"}],"edges":[]}`))
	mock.ExpectQuery(`^SELECT \* FROM "workflow_versions"`).WithArgs("wf-published", 1, 1).
		WillReturnRows(versionRows("wf-published", `{"nodes":[{"id":"a","type":"trigger_agent_call"}],"edges":[]}`))

	active, agentCall := workflow.WorkflowStatusActive, workflow.TriggerAgentCall
	page, err := NewWorkflowRepository(db).List(workflow.ListWorkflowsInput{WorkspaceID: "ws-1", Status: &active, TriggerType: &agentCall})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "wf-published" {
		t.Fatalf("items = %+v, want only the workflow whose published graph has the agent-call trigger", page.Items)
	}
	if page.Items[0].Graph.Nodes[0].Type != workflow.NodeType(workflow.TriggerManual) {
		t.Fatal("the list must still return the draft graph for the editor")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeletingAWorkflowDeletesItsUnpinnedVersionsInTheSameTransaction(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE "workflows" SET "deleted_at"=\$1 WHERE id = \$2 AND "workflows"."deleted_at" IS NULL$`).
		WithArgs(sqlmock.AnyArg(), "wf-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlPattern(deleteVersionsOfWorkflowSQL)).
		WithArgs("wf-1").
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	if err := NewWorkflowRepository(db).Delete("wf-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(deleteVersionsOfWorkflowSQL, "r.status IN ('running', 'waiting')") {
		t.Fatal("a version pinned by a live run must survive the delete")
	}
}
