package emailsending_repository

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	es "vozko/domain/emailsending"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestTakeDailySlotSQLShape(t *testing.T) {
	db, mock := mockDB(t)
	repo := NewDailyUsageRepository(db)
	query := regexp.QuoteMeta(dollarPlaceholders(takeDailySlotSQL))

	mock.ExpectQuery(query).WithArgs("dom-1", "2026-10-21", int64(500)).WillReturnRows(sqlmock.NewRows([]string{"used"}).AddRow(int64(7)))
	ok, err := repo.TakeDailySlot(context.Background(), "dom-1", "2026-10-21", 500)
	if err != nil || !ok {
		t.Fatalf("ok = %v err = %v", ok, err)
	}

	mock.ExpectQuery(query).WithArgs("dom-1", "2026-10-21", int64(500)).WillReturnRows(sqlmock.NewRows([]string{"used"}))
	ok, err = repo.TakeDailySlot(context.Background(), "dom-1", "2026-10-21", 500)
	if err != nil || ok {
		t.Fatalf("full day must refuse: ok = %v err = %v", ok, err)
	}

	mock.ExpectQuery(query).WithArgs("dom-1", "2026-10-21", es.Uncapped).WillReturnRows(sqlmock.NewRows([]string{"used"}).AddRow(int64(99999)))
	ok, err = repo.TakeDailySlot(context.Background(), "dom-1", "2026-10-21", es.Uncapped)
	if err != nil || !ok {
		t.Fatalf("uncapped: ok = %v err = %v", ok, err)
	}

	for _, refused := range []int64{0, -2} {
		ok, err = repo.TakeDailySlot(context.Background(), "dom-1", "2026-10-21", refused)
		if err != nil || ok {
			t.Fatalf("cap %d must refuse without a query: ok = %v err = %v", refused, ok, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func dollarPlaceholders(sql string) string {
	var b strings.Builder
	n := 0
	for _, r := range sql {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func TestTakeDailySlotPlaceholders(t *testing.T) {
	if n := regexp.MustCompile(`\?`).FindAllStringIndex(takeDailySlotSQL, -1); len(n) != 3 {
		t.Fatalf("take slot binds %d placeholders, want 3", len(n))
	}
	if n := regexp.MustCompile(`\?`).FindAllStringIndex(giveBackDailySlotSQL, -1); len(n) != 2 {
		t.Fatalf("give back binds %d placeholders, want 2", len(n))
	}
	if n := regexp.MustCompile(`\?`).FindAllStringIndex(healthWindowSQL, -1); len(n) != 4 {
		t.Fatalf("health window binds %d placeholders, want 4", len(n))
	}
}

func TestAppendEventDuplicateLeavesTheMessageAlone(t *testing.T) {
	db, mock := mockDB(t)
	repo := NewMessageRepository(db)
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cols := []string{"id", "workspace_id", "provider_message_id", "run_kind", "run_id", "recipient_id", "domain_id", "address", "status", "opens", "bot_opens", "clicks"}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "email_messages" WHERE id = \$1 LIMIT \$2 FOR UPDATE`).
		WithArgs("msg-1", 1).
		WillReturnRows(sqlmock.NewRows(cols).AddRow("msg-1", "ws-1", "ses-1", "campaign", "c-1", "", "dom-1", "a@b.com", "sent", 0, 0, 0))
	mock.ExpectExec(`INSERT INTO "email_message_events" .* ON CONFLICT DO NOTHING`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	got, err := repo.AppendEvent(context.Background(), "msg-1", es.DeliveryEvent{Kind: es.EventDelivery, ProviderMessageID: "ses-1", OccurredAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if got.New || got.Message.Status != es.MessageSent {
		t.Fatalf("got = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppendEventNewEventUpdatesUnderTheLock(t *testing.T) {
	db, mock := mockDB(t)
	repo := NewMessageRepository(db)
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cols := []string{"id", "workspace_id", "provider_message_id", "run_kind", "domain_id", "address", "status", "opens", "bot_opens", "clicks"}

	mock.ExpectBegin()
	mock.ExpectQuery(`FOR UPDATE`).WithArgs("msg-1", 1).
		WillReturnRows(sqlmock.NewRows(cols).AddRow("msg-1", "ws-1", "ses-1", "campaign", "dom-1", "a@b.com", "delivered", 2, 0, 0))
	mock.ExpectExec(`INSERT INTO "email_message_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "email_messages" SET .*"opens"=`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repo.AppendEvent(context.Background(), "msg-1", es.DeliveryEvent{Kind: es.EventOpen, ProviderMessageID: "ses-1", OccurredAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if !got.New || got.Message.Opens != 3 || !got.Effect.FirstOpen {
		t.Fatalf("got = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLinkHash(t *testing.T) {
	if LinkHash("") != "" {
		t.Fatal("no link, no hash")
	}
	if a, b := LinkHash("https://a.test/x"), LinkHash("https://a.test/y"); a == b || len(a) != 64 {
		t.Fatalf("hashes = %q %q", a, b)
	}
}
