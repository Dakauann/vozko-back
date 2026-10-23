package workflow_repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestConversationSwitchCancelsOnlyLiveRunsOfThatWorkspaceAndChannel(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(cancelConversationRunsSQL)).WithArgs("changed", "ws", "entry", "whatsapp", "trigger_message_received", "trigger_first_message", "agent:%").WillReturnRows(sqlmock.NewRows([]string{"run_id", "workspace_id", "workflow_id", "current_node_id"}).AddRow("old", "ws", "previous", "wait"))
	rows, err := NewConversationRunCanceller(db).CancelConversationRuns(context.Background(), "ws", "entry", "whatsapp", "changed")
	if err != nil || len(rows) != 1 || rows[0].RunID != "old" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
