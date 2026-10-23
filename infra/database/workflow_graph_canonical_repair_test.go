package database

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/workflow"
)

const builderWrittenGraph = `{"nodes":[{"id":"n27","type":"action_send_interactive","config":{"body":"Valores dos Materiais para aulas aquáticas:","sections":"[{\"Title\":\"Aulas Aquáticas\",\"Rows\":[{\"ID\":\"1\",\"Title\":\"Horários Aquáticas 🏊‍♀️\"},{\"ID\":\"2\",\"Title\":\"Materiais Aquáticos 🥽\"}]}]","list_button":"Escolha uma opção","interactive_type":"list"},"position":{"x":5096,"y":1186}},{"id":"n44","type":"wait_duration","config":{"seconds":1},"position":{"x":5484,"y":844}},{"id":"n28","type":"wait_duration","config":{"seconds":1},"position":{"x":5484,"y":1044}}],"edges":[{"label":"2","source":"n27","target":"n28"},{"label":"1","source":"n27","target":"n44"}]}`

const canonicalRealSections = `[{"title":"Aulas Aquáticas","rows":[{"id":"1","title":"Horários Aquáticas 🏊‍♀️","description":""},{"id":"2","title":"Materiais Aquáticos 🥽","description":""}]}]`

func TestTheGraphCanonicalRepairScansInKeysetBatchesFromTheNilUUID(t *testing.T) {
	for _, query := range []string{draftGraphsWithStructuredConfigSQL, versionGraphsWithStructuredConfigSQL} {
		for _, fragment := range []string{"id > ?", "jsonb_path_exists(graph, ?::jsonpath)", "ORDER BY id", "LIMIT ?"} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("selection misses %q:\n%s", fragment, query)
			}
		}
		if got := strings.Count(query, "?"); got != 3 {
			t.Fatalf("selection placeholders = %d, want 3:\n%s", got, query)
		}
	}
	if got := strings.Count(rewriteDraftGraphSQL, "?"); got != 3 || !strings.Contains(rewriteDraftGraphSQL, "graph = ?::jsonb WHERE id = ? AND graph = ?::jsonb") {
		t.Fatalf("the draft rewrite must only replace the graph it read:\n%s", rewriteDraftGraphSQL)
	}
	if got := strings.Count(rewriteVersionGraphSQL, "?"); got != 4 || !strings.Contains(rewriteVersionGraphSQL, "graph_hash = ?") || !strings.Contains(rewriteVersionGraphSQL, "AND graph = ?::jsonb") {
		t.Fatalf("the version rewrite must refresh the hash and only replace the graph it read:\n%s", rewriteVersionGraphSQL)
	}
	if workflowVersionRepairFirstKey != uuid.Nil.String() {
		t.Fatalf("the scan must start from the nil UUID, got %q", workflowVersionRepairFirstKey)
	}
}

func TestTheGraphCanonicalRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "wf_canonical_structured_configs" {
			return
		}
	}
	t.Fatal("the structured config repair must run at boot")
}

func canonicalOf(t *testing.T, raw string) (string, string) {
	t.Helper()
	out, changed, err := workflow.CanonicalizeGraphJSON([]byte(raw))
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	var g workflow.Graph
	if err := json.Unmarshal(out, &g); err != nil {
		t.Fatal(err)
	}
	return string(out), workflow.GraphHash(&g)
}

func TestTheGraphCanonicalRepairRewritesOnlyGraphsThatChange(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	path := workflow.StructuredConfigGraphPath()
	canonical, hash := canonicalOf(t, builderWrittenGraph)

	mock.ExpectQuery(sqlOf(draftGraphsWithStructuredConfigSQL)).WithArgs(workflowVersionRepairFirstKey, path, workflowGraphCanonicalBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}).
			AddRow("wf-a", builderWrittenGraph).
			AddRow("wf-b", canonical).
			AddRow("wf-c", "{not json"))
	mock.ExpectExec(sqlOf(rewriteDraftGraphSQL)).WithArgs(canonical, "wf-a", builderWrittenGraph).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(sqlOf(draftGraphsWithStructuredConfigSQL)).WithArgs("wf-c", path, workflowGraphCanonicalBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}))
	mock.ExpectQuery(sqlOf(versionGraphsWithStructuredConfigSQL)).WithArgs(workflowVersionRepairFirstKey, path, workflowGraphCanonicalBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}).AddRow("v-a", builderWrittenGraph))
	mock.ExpectExec(sqlOf(rewriteVersionGraphSQL)).WithArgs(canonical, hash, "v-a", builderWrittenGraph).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(sqlOf(versionGraphsWithStructuredConfigSQL)).WithArgs("v-a", path, workflowGraphCanonicalBatch).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}))

	if err := canonicalizeStoredGraphs(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheGraphCanonicalRepairAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	schemaName := "wf_graph_canonical_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, ddl := range []string{
		`CREATE SCHEMA ` + schemaName,
		`SET LOCAL search_path TO ` + schemaName,
		`CREATE TABLE workflows (id uuid PRIMARY KEY, graph jsonb, published_version integer, deleted_at timestamptz)`,
		`CREATE TABLE workflow_versions (id uuid PRIMARY KEY, workflow_id uuid NOT NULL, version integer NOT NULL, graph jsonb NOT NULL, graph_hash varchar(64) NOT NULL)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	var before workflow.Graph
	if err := json.Unmarshal([]byte(builderWrittenGraph), &before); err != nil {
		t.Fatal(err)
	}
	workflowID, versionID, untouchedID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	plain := `{"nodes":[{"id":"t","type":"trigger_message_received","config":{}}],"edges":[]}`
	for _, seed := range []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO workflows (id, graph, published_version) VALUES (?, ?::jsonb, 1)`, []interface{}{workflowID, builderWrittenGraph}},
		{`INSERT INTO workflows (id, graph) VALUES (?, ?::jsonb)`, []interface{}{untouchedID, plain}},
		{`INSERT INTO workflow_versions (id, workflow_id, version, graph, graph_hash) VALUES (?, ?, 1, ?::jsonb, ?)`, []interface{}{versionID, workflowID, builderWrittenGraph, workflow.GraphHash(&before)}},
	} {
		if err := tx.Exec(seed.sql, seed.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := canonicalizeStoredGraphs(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := canonicalizeStoredGraphs(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}

	readGraph := func(query, id string) workflow.Graph {
		var raw string
		if err := tx.Raw(query, id).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		var g workflow.Graph
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	draft := readGraph(`SELECT graph::text FROM workflows WHERE id = ?`, workflowID)
	version := readGraph(`SELECT graph::text FROM workflow_versions WHERE id = ?`, versionID)
	for name, g := range map[string]workflow.Graph{"draft": draft, "version": version} {
		if got := g.FindNode("n27").Config["sections"]; got != canonicalRealSections {
			t.Fatalf("%s sections = %v", name, got)
		}
		if len(g.Edges) != 2 {
			t.Fatalf("%s edges = %v", name, g.Edges)
		}
	}
	var storedHash string
	tx.Raw(`SELECT graph_hash FROM workflow_versions WHERE id = ?`, versionID).Scan(&storedHash)
	if storedHash != workflow.GraphHash(&version) || storedHash != workflow.GraphHash(&draft) {
		t.Fatalf("hash = %s, want the read-side hash of both graphs so the workflow still reads as published", storedHash)
	}
	var untouched string
	tx.Raw(`SELECT graph::text FROM workflows WHERE id = ?`, untouchedID).Scan(&untouched)
	if !strings.Contains(untouched, `"trigger_message_received"`) {
		t.Fatalf("a graph without structured configs changed: %s", untouched)
	}
}
