package workflow_repository

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

func TestTheWorkspacesWithAnActiveTriggerComeFromThePublishedGraphs(t *testing.T) {
	if got := strings.Count(workspacesWithActiveTriggerSQL, "?"); got != 1 {
		t.Fatalf("placeholders = %d", got)
	}
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(workspacesWithActiveTriggerSQL)).
		WithArgs(`{"nodes":[{"type":"trigger_birthday"}]}`).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}).AddRow("ws-1").AddRow("ws-2"))

	got, err := NewWorkflowRepository(db).(*repository).WorkspacesWithActiveTrigger(workflow.TriggerBirthday)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "ws-1" || got[1] != "ws-2" {
		t.Fatalf("workspaces = %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"w.status = 'active'", "w.deleted_at IS NULL", "v.version = w.published_version", "v.graph @> CAST(? AS jsonb)"} {
		if !strings.Contains(workspacesWithActiveTriggerSQL, fragment) {
			t.Fatalf("statement misses %q:\n%s", fragment, workspacesWithActiveTriggerSQL)
		}
	}
}

func TestRunsWithoutAConversationAreNeverHiddenByTheInboxScope(t *testing.T) {
	clause, _ := visibleRunEntries("ws-1", conversation.NothingVisible())
	want := "workflow_runs.entry_type IN ('" + workflow.EntryTypeSIPCall + "', '" + workflow.EntryTypeLead + "')"
	if !strings.Contains(clause, want) {
		t.Fatalf("clause = %s", clause)
	}
}

func TestOnlyWorkspacesWhosePublishedActiveGraphHasTheTriggerAreSweptAgainstPostgres(t *testing.T) {
	f := newSQLFixture(t)
	one := 1
	live := f.workflow(&one, graphWithNode("d", workflow.NodeTypeTriggerMessageReceived))
	f.version(live, 1, graphWithNode("p", workflow.NodeTypeTriggerBirthday))
	draftOnly := f.workflow(&one, graphWithNode("d", workflow.NodeTypeTriggerBirthday))
	f.version(draftOnly, 1, graphWithNode("p", workflow.NodeTypeTriggerMessageReceived))
	paused := f.workflow(&one, graphWithNode("d", workflow.NodeTypeTriggerBirthday))
	f.version(paused, 1, graphWithNode("p", workflow.NodeTypeTriggerBirthday))
	f.exec(`UPDATE workflows SET workspace_id = ? WHERE id = ?`, uuid.NewString(), draftOnly)
	f.exec(`UPDATE workflows SET status = 'paused', workspace_id = ? WHERE id = ?`, uuid.NewString(), paused)

	got, err := NewWorkflowRepository(f.db).(*repository).WorkspacesWithActiveTrigger(workflow.TriggerBirthday)
	if err != nil || len(got) != 1 || got[0] != f.workspace {
		t.Fatalf("workspaces = %v, err = %v, want only %s", got, err, f.workspace)
	}
}
