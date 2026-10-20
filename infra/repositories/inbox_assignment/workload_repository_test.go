package inbox_assignment_repository

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestOpenConversations_CountsHumanOwnedUnfinishedEntriesPerMember(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM inbox_assignments ia JOIN \(SELECT .+ UNION ALL .+\) e ON e\.entry_id = ia\.entry_id AND e\.entry_type = ia\.entry_type `+
		`WHERE ia\.workspace_id = \$1 AND ia\.assignee_kind = \$2 AND ia\.assigned_user_id IN \(\$3,\$4\) GROUP BY ia\.assigned_user_id`).
		WithArgs("ws-1", "human", "ana", "bob").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "open_count"}).AddRow("ana", 3))

	got, err := NewWorkloadRepository(db).OpenConversations("ws-1", []string{"ana", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if got["ana"] != 3 || got["bob"] != 0 || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenConversations_NoMembersSkipsTheQuery(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	got, err := NewWorkloadRepository(db).OpenConversations("ws-1", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenConversations_ReturnsTheReadError(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM inbox_assignments`).WillReturnError(errors.New("db down"))
	if _, err := NewWorkloadRepository(db).OpenConversations("ws-1", []string{"ana"}); err == nil {
		t.Fatal("a failed read must surface so the roulette can fail closed")
	}
}

func TestOpenConversationsSQL_BindsOnlyItsOwnPlaceholders(t *testing.T) {
	if got := strings.Count(openConversationsSQL, "?"); got != 3 {
		t.Fatalf("the statement must bind exactly workspace, kind and members, found %d placeholders", got)
	}
}
