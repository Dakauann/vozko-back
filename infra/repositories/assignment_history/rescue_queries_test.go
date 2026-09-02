package assignment_history_repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	ia "vozko/domain/inbox_assignment"
)

func newHistoryDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
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

// The sweep's candidate query. Oldest first, so a saturated batch always makes
// progress on whoever has been waiting longest rather than re-picking the same
// arbitrary page every tick.
func TestListOpenOlderThan_QueryShape(t *testing.T) {
	db, mock, sqlDB := newHistoryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "assignment_history" ` +
		`WHERE workspace_id IN \(.*\) AND ended_at IS NULL AND trigger IN \(.*\) AND started_at < \$\d+ ` +
		`ORDER BY started_at ASC LIMIT`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := New(db).ListOpenOlderThan([]string{"ws-1", "ws-2"}, ia.RescueCandidateTriggers, time.Now(), 200)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// No eligible workspaces means no query at all — the sweep ticks every minute
// and most deployments have nobody in the last_seen mode.
func TestListOpenOlderThan_NoWorkspacesSkipsTheQuery(t *testing.T) {
	db, mock, sqlDB := newHistoryDB(t)
	defer sqlDB.Close()

	got, err := New(db).ListOpenOlderThan(nil, ia.RescueCandidateTriggers, time.Now(), 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// One query, not two. The sweep calls this once per stalled conversation, so a
// second round-trip here is a per-candidate cost on the hungriest path.
//
// The quoting is load-bearing and asserted deliberately: TRIGGER is a reserved
// word in SQL, and this pins that GORM emits it quoted.
func TestCountRescuesSinceHandout_IsASingleQuery(t *testing.T) {
	db, mock, sqlDB := newHistoryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT "trigger" FROM "assignment_history" ` +
		`WHERE workspace_id = \$1 AND entry_id = \$2 AND entry_type = \$3 ` +
		`ORDER BY started_at DESC LIMIT`).
		WillReturnRows(sqlmock.NewRows([]string{"trigger"}))

	if _, err := New(db).CountRescuesSinceHandout("ws-1", "e-1", "whatsapp"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The chain is bounded by the roulette hand-out that opened it: rescues from a
// previous chain must not count against this one, or a long-lived conversation
// would exhaust its ring on the first stall.
func TestCountRescuesSinceHandout_StopsAtTheHandout(t *testing.T) {
	cases := map[string]struct {
		newestFirst []string
		want        int
	}{
		"fresh handout, no hops":  {[]string{ia.TriggerInboundRR}, 0},
		"two hops this chain":     {[]string{ia.TriggerRescue, ia.TriggerRescue, ia.TriggerInboundRR}, 2},
		"previous chain excluded": {[]string{ia.TriggerRescue, ia.TriggerInboundRR, ia.TriggerRescue, ia.TriggerRescue, ia.TriggerInboundRR}, 1},
		"manual reassign ignored": {[]string{ia.TriggerManual, ia.TriggerRescue, ia.TriggerInboundRR}, 1},
		"no handout in history":   {[]string{ia.TriggerManual, ia.TriggerOpen}, 0},
		"empty history":           {nil, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock, sqlDB := newHistoryDB(t)
			defer sqlDB.Close()

			rows := sqlmock.NewRows([]string{"trigger"})
			for _, trig := range tc.newestFirst {
				rows = rows.AddRow(trig)
			}
			mock.ExpectQuery(`SELECT "trigger"`).WillReturnRows(rows)

			got, err := New(db).CountRescuesSinceHandout("ws-1", "e-1", "whatsapp")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
