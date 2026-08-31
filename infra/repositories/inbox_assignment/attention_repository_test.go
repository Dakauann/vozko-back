package inbox_assignment_repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newAttentionDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

// The rescue's correctness gate. Two signals, and the shape of each one is what
// decides whether an agent's conversation is taken away from them:
//
//   - read_by = the ASSIGNED agent. A supervisor skimming the inbox is not the
//     owner attending it, so "read by anyone" would silently suppress rescues
//     the customer is still waiting on.
//   - any OUTBOUND message. Somebody answered, so the customer is not waiting.
//
// The soft-delete guard is asserted too: a deleted message is not attention.
func TestAttendedSince_QueryShape(t *testing.T) {
	db, mock, sqlDB := newAttentionDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "conversation_messages" ` +
		`WHERE \(entry_id = \$1 AND entry_type = \$2\) AND ` +
		`\(\(read = \$3 AND read_by = \$4 AND read_at >= \$5\) OR ` +
		`\(created_at >= \$6 AND \(direction = \$7 OR ` +
		`\(direction = \$8 AND message_type NOT IN .*\)\)\)\) AND ` +
		`"conversation_messages"."deleted_at" IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	if _, err := NewAttentionRepository(db).AttendedSince("e-1", "whatsapp", "u-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAttendedSince_ReportsWhetherAnyRowMatched(t *testing.T) {
	cases := map[string]struct {
		count int64
		want  bool
	}{
		"nobody touched it": {0, false},
		"somebody did":      {1, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock, sqlDB := newAttentionDB(t)
			defer sqlDB.Close()

			mock.ExpectQuery(`SELECT count`).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.count))

			got, err := NewAttentionRepository(db).AttendedSince("e-1", "whatsapp", "u-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// A malformed ask must not reach the database, and must not report the
// conversation as attended — reporting true would suppress a rescue the
// customer is waiting on.
func TestAttendedSince_MissingEntrySkipsTheQuery(t *testing.T) {
	db, mock, sqlDB := newAttentionDB(t)
	defer sqlDB.Close()

	repo := NewAttentionRepository(db)
	for _, tc := range [][2]string{{"", "whatsapp"}, {"e-1", ""}} {
		got, err := repo.AttendedSince(tc[0], tc[1], "u-1", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got {
			t.Fatal("a malformed ask must not report the conversation as attended")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
