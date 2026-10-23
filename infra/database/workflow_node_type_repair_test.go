package database

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/workflow"
)

const legacyMeetingGraph = `{"nodes":[{"id":"m1","type":"action_schedule_meeting","config":{"title":"Visita","create_google_meet":"false"},"pinned":{"output":{"google_event_id":"g-1"}},"position":{"x":1,"y":2}},{"id":"m2","type":"action_reschedule_meeting","config":{"event_id":"{{nodes.m1.google_event_id}}"},"position":{"x":3,"y":4}}],"edges":[{"source":"m1","target":"m2","label":"sucesso"}]}`

func TestTheLegacyNodeTypeRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "wf_canonical_node_types" {
			return
		}
	}
	t.Fatal("the legacy node type repair must run at boot")
}

func TestTheRunStepRenameIsBoundedAndBackedByItsPartialIndex(t *testing.T) {
	predicate := legacyRunStepTypePredicate()
	for _, legacy := range workflow.LegacyNodeTypes() {
		if !strings.Contains(predicate, "'"+string(legacy)+"'") {
			t.Fatalf("predicate %s misses %s", predicate, legacy)
		}
	}
	rename := renameLegacyRunStepTypesSQL()
	if got := strings.Count(rename, "?"); got != 1 || !strings.Contains(rename, "LIMIT ?") {
		t.Fatalf("the rename must take only the batch size as a placeholder:\n%s", rename)
	}
	if !strings.Contains(rename, "WHERE "+predicate+" LIMIT ?") {
		t.Fatalf("the rename must select with the exact predicate of its partial index:\n%s", rename)
	}
	for _, legacy := range workflow.LegacyNodeTypes() {
		if !strings.Contains(rename, "WHEN '"+string(legacy)+"' THEN '"+string(legacy.Canonical())+"'") {
			t.Fatalf("the rename misses %s:\n%s", legacy, rename)
		}
	}
	index := declaredConcurrentIndex(t, LegacyRunStepTypeIndex)
	if !strings.Contains(strings.Join(strings.Fields(index.sql), " "), "ON workflow_run_logs (id) WHERE "+predicate) {
		t.Fatalf("the partial index must carry the rename predicate:\n%s", index.sql)
	}
}

func TestTheBootRepairRenamesGraphsAndNeverTouchesRunSteps(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	path := workflow.LegacyNodeTypeGraphPath()
	canonical, hash := canonicalOf(t, legacyMeetingGraph)

	mock.ExpectQuery(sqlOf(draftGraphsWithStructuredConfigSQL)).WithArgs(workflowVersionRepairFirstKey, path, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}).AddRow("wf-a", legacyMeetingGraph))
	mock.ExpectExec(sqlOf(rewriteDraftGraphSQL)).WithArgs(canonical, "wf-a", legacyMeetingGraph).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(sqlOf(draftGraphsWithStructuredConfigSQL)).WithArgs("wf-a", path, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}))
	mock.ExpectQuery(sqlOf(versionGraphsWithStructuredConfigSQL)).WithArgs(workflowVersionRepairFirstKey, path, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}).AddRow("v-a", legacyMeetingGraph))
	mock.ExpectExec(sqlOf(rewriteVersionGraphSQL)).WithArgs(canonical, hash, "v-a", legacyMeetingGraph).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(sqlOf(versionGraphsWithStructuredConfigSQL)).WithArgs("v-a", path, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "graph"}))

	if err := renameLegacyNodeTypes(db, 2); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheRunStepRenameWaitsWhileItsIndexIsMissingOrInvalid(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	mock.ExpectQuery(sqlOf(validIndexSQL)).WithArgs(LegacyRunStepTypeIndex).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	if err := renameLegacyRunStepTypesOnceIndexed(db, 3); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheRunStepRenameRefusesWhenItCannotTellWhetherItsIndexIsValid(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	mock.ExpectQuery(sqlOf(validIndexSQL)).WithArgs(LegacyRunStepTypeIndex).WillReturnError(errors.New("catalog unavailable"))

	if err := renameLegacyRunStepTypesOnceIndexed(db, 3); err == nil {
		t.Fatal("want an error, never a rename without knowing the index is there")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheRunStepRenameDrainsInBatchesOnceItsIndexIsValid(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	mock.ExpectQuery(sqlOf(validIndexSQL)).WithArgs(LegacyRunStepTypeIndex).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(sqlOf(renameLegacyRunStepTypesSQL())).WithArgs(3).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(sqlOf(renameLegacyRunStepTypesSQL())).WithArgs(3).WillReturnResult(sqlmock.NewResult(0, 1))

	if err := renameLegacyRunStepTypesOnceIndexed(db, 3); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheLegacyNodeTypeRepairAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	schemaName := "wf_node_types_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	index := declaredConcurrentIndex(t, LegacyRunStepTypeIndex)
	for _, ddl := range []string{
		`CREATE SCHEMA ` + schemaName,
		`SET LOCAL search_path TO ` + schemaName,
		`CREATE TABLE workflows (id uuid PRIMARY KEY, graph jsonb, published_version integer, deleted_at timestamptz)`,
		`CREATE TABLE workflow_versions (id uuid PRIMARY KEY, workflow_id uuid NOT NULL, version integer NOT NULL, graph jsonb NOT NULL, graph_hash varchar(64) NOT NULL)`,
		`CREATE TABLE workflow_run_logs (id uuid PRIMARY KEY, run_id uuid NOT NULL, node_id varchar(100) NOT NULL, node_type varchar(40) NOT NULL)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	var before workflow.Graph
	if err := json.Unmarshal([]byte(legacyMeetingGraph), &before); err != nil {
		t.Fatal(err)
	}
	staleHash := strings.Repeat("0", 64)
	drafts := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	versionID, untouchedID, runID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	plain := `{"nodes":[{"id":"t","type":"trigger_message_received","config":{}}],"edges":[]}`
	seeds := []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO workflows (id, graph) VALUES (?, ?::jsonb)`, []interface{}{untouchedID, plain}},
		{`INSERT INTO workflow_versions (id, workflow_id, version, graph, graph_hash) VALUES (?, ?, 1, ?::jsonb, ?)`, []interface{}{versionID, drafts[0], legacyMeetingGraph, staleHash}},
		{`INSERT INTO workflow_run_logs (id, run_id, node_id, node_type) VALUES (?, ?, 'other', 'action_send_text')`, []interface{}{uuid.NewString(), runID}},
	}
	for _, id := range drafts {
		seeds = append(seeds, struct {
			sql  string
			args []interface{}
		}{`INSERT INTO workflows (id, graph, published_version) VALUES (?, ?::jsonb, 1)`, []interface{}{id, legacyMeetingGraph}})
	}
	for i := 0; i < 5; i++ {
		legacy := workflow.NodeTypeActionScheduleMeetingLegacy
		if i%2 == 1 {
			legacy = workflow.NodeTypeActionRescheduleMeetingLegacy
		}
		seeds = append(seeds, struct {
			sql  string
			args []interface{}
		}{`INSERT INTO workflow_run_logs (id, run_id, node_id, node_type) VALUES (?, ?, 'm', ?)`, []interface{}{uuid.NewString(), runID, string(legacy)}})
	}
	for _, seed := range seeds {
		if err := tx.Exec(seed.sql, seed.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	countLegacySteps := func() int {
		var n int
		if err := tx.Raw(`SELECT count(*) FROM workflow_run_logs WHERE ` + legacyRunStepTypePredicate()).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := renameLegacyNodeTypes(tx, 2); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := renameLegacyRunStepTypesOnceIndexed(tx, 2); err != nil {
		t.Fatalf("run step rename without its index: %v", err)
	}
	if got := countLegacySteps(); got != 5 {
		t.Fatalf("legacy run steps = %d, want all 5 untouched while the index is missing", got)
	}
	if err := tx.Exec(strings.Replace(index.sql, "CONCURRENTLY ", "", 1)).Error; err != nil {
		t.Fatalf("index: %v", err)
	}

	var plan []string
	if err := tx.Exec(`SET LOCAL enable_seqscan = off`).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Raw(`EXPLAIN ` + strings.Replace(renameLegacyRunStepTypesSQL(), "LIMIT ?", "LIMIT 2", 1)).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), LegacyRunStepTypeIndex) {
		t.Fatalf("the rename must be able to read through its partial index:\n%s", strings.Join(plan, "\n"))
	}

	for range 2 {
		if err := renameLegacyNodeTypes(tx, 2); err != nil {
			t.Fatalf("repair: %v", err)
		}
		if err := renameLegacyRunStepTypesOnceIndexed(tx, 2); err != nil {
			t.Fatalf("run step rename: %v", err)
		}
	}
	if got := countLegacySteps(); got != 0 {
		t.Fatalf("legacy run steps = %d after the indexed rename", got)
	}

	readGraph := func(query, id string) workflow.Graph {
		var raw string
		if err := tx.Raw(query, id).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "meeting") {
			t.Fatalf("a legacy type survived: %s", raw)
		}
		var g workflow.Graph
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	for _, id := range drafts {
		g := readGraph(`SELECT graph::text FROM workflows WHERE id = ?`, id)
		if g.FindNode("m1").Type != workflow.NodeTypeActionCreateCalendarEvent || g.FindNode("m1").Pinned == nil || len(g.Edges) != 1 {
			t.Fatalf("draft %s = %+v", id, g)
		}
	}
	version := readGraph(`SELECT graph::text FROM workflow_versions WHERE id = ?`, versionID)
	if version.FindNode("m2").Type != workflow.NodeTypeActionRescheduleCalendarEvent || version.FindNode("m2").Config["event_id"] != "{{nodes.m1.google_event_id}}" {
		t.Fatalf("version = %+v", version)
	}
	var storedHash string
	tx.Raw(`SELECT graph_hash FROM workflow_versions WHERE id = ?`, versionID).Scan(&storedHash)
	if storedHash != workflow.GraphHash(&version) || storedHash != workflow.GraphHash(&before) {
		t.Fatalf("hash = %s, want the read-side hash so the workflow still reads as published", storedHash)
	}
	var untouched string
	tx.Raw(`SELECT graph::text FROM workflows WHERE id = ?`, untouchedID).Scan(&untouched)
	if !strings.Contains(untouched, `"trigger_message_received"`) {
		t.Fatalf("a graph without legacy types changed: %s", untouched)
	}
	var steps []struct {
		NodeType string
		Count    int
	}
	tx.Raw(`SELECT node_type, count(*) AS count FROM workflow_run_logs GROUP BY node_type ORDER BY node_type`).Scan(&steps)
	want := map[string]int{
		string(workflow.NodeTypeActionCreateCalendarEvent):     3,
		string(workflow.NodeTypeActionRescheduleCalendarEvent): 2,
		string(workflow.NodeTypeActionSendText):                1,
	}
	if len(steps) != len(want) {
		t.Fatalf("run step types = %+v, want %v", steps, want)
	}
	for _, step := range steps {
		if want[step.NodeType] != step.Count {
			t.Fatalf("run step types = %+v, want %v", steps, want)
		}
	}
}
