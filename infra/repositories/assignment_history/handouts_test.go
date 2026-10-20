package assignment_history_repository

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLastHandouts_ReadsTheLatestStartPerMember(t *testing.T) {
	db, mock, sqlDB := newHistoryDB(t)
	defer sqlDB.Close()

	at := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT assigned_actor_id AS user_id, MAX\(started_at\) AS last_at FROM "assignment_history" `+
		`WHERE workspace_id = \$1 AND assigned_actor_id IN \(\$2,\$3\) GROUP BY "assigned_actor_id"`).
		WithArgs("ws-1", "ana", "bob").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "last_at"}).AddRow("ana", at))

	got, err := NewHandoutReader(db).LastHandouts("ws-1", []string{"ana", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["ana"].Equal(at) || !got["bob"].IsZero() {
		t.Fatalf("got %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLastHandouts_NoMembersSkipsTheQuery(t *testing.T) {
	db, mock, sqlDB := newHistoryDB(t)
	defer sqlDB.Close()

	if got, err := NewHandoutReader(db).LastHandouts("ws-1", nil); err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLastHandouts_ReturnsTheReadError(t *testing.T) {
	db, mock, sqlDB := newHistoryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`assignment_history`).WillReturnError(errors.New("db down"))
	if _, err := NewHandoutReader(db).LastHandouts("ws-1", []string{"ana"}); err == nil {
		t.Fatal("a failed read must surface so the roulette can fail closed")
	}
}
