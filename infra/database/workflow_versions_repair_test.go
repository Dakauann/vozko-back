package database

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/workflow"
)

func TestTheVersionRepairOnlyTouchesRunningWorkflowsWithoutAVersion(t *testing.T) {
	for _, fragment := range []string{
		"deleted_at IS NULL",
		"status IN ('active', 'paused')",
		"published_version IS NULL",
		"id > ?",
		"ORDER BY id",
		"LIMIT ?",
	} {
		if !strings.Contains(unpublishedRunningWorkflowsSQL, fragment) {
			t.Fatalf("selection misses %q:\n%s", fragment, unpublishedRunningWorkflowsSQL)
		}
	}
	if got := strings.Count(unpublishedRunningWorkflowsSQL, "?"); got != 2 {
		t.Fatalf("selection placeholders = %d, want 2", got)
	}
	if got := strings.Count(insertFirstVersionSQL, "?"); got != 5 || !strings.Contains(insertFirstVersionSQL, "ON CONFLICT (workflow_id, version) DO NOTHING") {
		t.Fatalf("insert must bind 5 values and be idempotent:\n%s", insertFirstVersionSQL)
	}
	if got := strings.Count(markFirstVersionPublishedSQL, "?"); got != 1 || !strings.Contains(markFirstVersionPublishedSQL, "published_version IS NULL") {
		t.Fatalf("the mark must bind the id only and never overwrite a published version:\n%s", markFirstVersionPublishedSQL)
	}
}

func sqlOf(query string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(query), `\?`, `\$[0-9]+`) + "$"
}

func TestTheVersionRepairPublishesVersionOneWithTheGraphHashTheReadSideComputes(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	graphJSON := `{"nodes":[{"id":"t","type":"trigger_webhook","position":{"x":0,"y":0},"config":{"b":1,"a":"x"}}],"edges":[]}`
	var g workflow.Graph
	if err := json.Unmarshal([]byte(graphJSON), &g); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(sqlOf(unpublishedRunningWorkflowsSQL)).WithArgs(workflowVersionRepairFirstKey, workflowVersionRepairBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "graph"}).
			AddRow("wf-a", "messages", graphJSON).
			AddRow("wf-broken", "messages", "{not json"))
	mock.ExpectExec(sqlOf(insertFirstVersionSQL)).WithArgs(sqlmock.AnyArg(), "wf-a", "messages", graphJSON, workflow.GraphHash(&g)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlOf(markFirstVersionPublishedSQL)).WithArgs("wf-a").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(sqlOf(unpublishedRunningWorkflowsSQL)).WithArgs("wf-broken", workflowVersionRepairBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "graph"}))

	if err := publishUnversionedWorkflows(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheVersionRepairNeverPublishesAPin(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	draft := `{"nodes":[{"id":"h","type":"action_http_request","position":{"x":0,"y":0},"config":{},"pinned":{"output":{},"source":"manual","pinned_at":"2026-10-20T12:00:00Z"}}],"edges":[]}`
	var g workflow.Graph
	if err := json.Unmarshal([]byte(draft), &g); err != nil {
		t.Fatal(err)
	}
	stripped, _ := json.Marshal(workflow.StripPins(g))
	mock.ExpectQuery(sqlOf(unpublishedRunningWorkflowsSQL)).WithArgs(workflowVersionRepairFirstKey, workflowVersionRepairBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "graph"}).AddRow("wf-a", "messages", draft))
	mock.ExpectExec(sqlOf(insertFirstVersionSQL)).WithArgs(sqlmock.AnyArg(), "wf-a", "messages", string(stripped), workflow.GraphHash(&g)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlOf(markFirstVersionPublishedSQL)).WithArgs("wf-a").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(sqlOf(unpublishedRunningWorkflowsSQL)).WithArgs("wf-a", workflowVersionRepairBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "graph"}))

	if err := publishUnversionedWorkflows(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheVersionRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "wf_publish_running_workflows" {
			return
		}
	}
	t.Fatal("the version repair must run at boot")
}

func TestTheVersionRepairAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	schemaName := "wf_version_repair_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, ddl := range []string{
		`CREATE SCHEMA ` + schemaName,
		`SET LOCAL search_path TO ` + schemaName,
		`CREATE TABLE workflows (id uuid PRIMARY KEY, status varchar(20), type varchar(20) NOT NULL DEFAULT 'messages', graph jsonb, published_version integer, deleted_at timestamptz)`,
		`CREATE TABLE workflow_versions (id uuid PRIMARY KEY, workflow_id uuid NOT NULL, version integer NOT NULL, type varchar(20) NOT NULL, graph jsonb NOT NULL, graph_hash varchar(64) NOT NULL, published_at timestamptz NOT NULL, published_by varchar(100), created_at timestamptz)`,
		`CREATE UNIQUE INDEX ON workflow_versions (workflow_id, version)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	graph := `{"nodes":[{"id":"t","type":"trigger_manual","position":{"x":0,"y":0},"config":{}}],"edges":[]}`
	seed := func(status string, published interface{}, deleted bool) string {
		id := uuid.NewString()
		deletedAt := interface{}(nil)
		if deleted {
			deletedAt = "2026-01-01"
		}
		if err := tx.Exec(`INSERT INTO workflows (id, status, graph, published_version, deleted_at) VALUES (?, ?, ?::jsonb, ?, ?)`, id, status, graph, published, deletedAt).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		return id
	}
	active, paused, draft := seed("active", nil, false), seed("paused", nil, false), seed("draft", nil, false)
	archived, deleted, already := seed("archived", nil, false), seed("active", nil, true), seed("active", 4, false)

	if err := publishRunningWorkflows(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := publishRunningWorkflows(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}

	versions := func(id string) int {
		var n int
		tx.Raw(`SELECT count(*) FROM workflow_versions WHERE workflow_id = ?`, id).Scan(&n)
		return n
	}
	published := func(id string) *int {
		var v *int
		tx.Raw(`SELECT published_version FROM workflows WHERE id = ?`, id).Scan(&v)
		return v
	}
	for name, id := range map[string]string{"active": active, "paused": paused} {
		if versions(id) != 1 || published(id) == nil || *published(id) != 1 {
			t.Fatalf("%s: versions = %d published = %v, want version 1 published", name, versions(id), published(id))
		}
	}
	for name, id := range map[string]string{"draft": draft, "archived": archived, "deleted": deleted, "already published": already} {
		if versions(id) != 0 {
			t.Fatalf("%s: got a version it must not have", name)
		}
	}
	if v := published(already); v == nil || *v != 4 {
		t.Fatalf("an existing published version was overwritten: %v", v)
	}
	var hash string
	tx.Raw(`SELECT graph_hash FROM workflow_versions WHERE workflow_id = ?`, active).Scan(&hash)
	var g workflow.Graph
	_ = json.Unmarshal([]byte(graph), &g)
	if hash != workflow.GraphHash(&g) {
		t.Fatalf("hash = %s, want the read-side hash so the draft reads as published", hash)
	}
}
