package lead_campaign_send

import (
	"database/sql/driver"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type argCounter struct{ total *int }

func (a argCounter) Match(driver.Value) bool {
	*a.total++
	return true
}

func newCapturingDB(t *testing.T, captured *[]string) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		*captured = append(*captured, actual)
		return nil
	})
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func leadIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprint(i))).String()
	}
	return ids
}

func TestGetLastSendTimesBatch_BindsOneArrayForSeventyThousandLeads(t *testing.T) {
	var captured []string
	db, mock := newCapturingDB(t, &captured)
	ids := leadIDs(70_000)
	sentAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var args int
	mock.ExpectQuery("lead_campaign_sends").
		WithArgs(argCounter{&args}, argCounter{&args}).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "last_sent_at"}).AddRow(ids[3], sentAt))

	got, err := NewRepository(db).GetLastSendTimesBatch(ids, "7f9c2ba4-e88f-4d0b-a7a2-1c2f3e4d5a6b")
	if err != nil {
		t.Fatal(err)
	}
	if !got[ids[3]].Equal(sentAt) || len(got) != 1 {
		t.Fatalf("unexpected result %v", got)
	}
	if len(captured) != 1 {
		t.Fatalf("want one query, got %d", len(captured))
	}
	if n := len(regexp.MustCompile(`\$\d+`).FindAllString(captured[0], -1)); n != 2 || args != 2 {
		t.Fatalf("want 2 placeholders and 2 args for 70,000 leads, got %d and %d:\n%.300s", n, args, captured[0])
	}
	if !regexp.MustCompile(`lead_id = ANY\(\$1::uuid\[\]\)`).MatchString(captured[0]) {
		t.Fatalf("lead ids must bind as one uuid array:\n%s", captured[0])
	}
}

func TestGetLastSendTimesBatch_NothingToLookUp(t *testing.T) {
	var captured []string
	db, _ := newCapturingDB(t, &captured)
	got, err := NewRepository(db).GetLastSendTimesBatch([]string{"bogus", ""}, "7f9c2ba4-e88f-4d0b-a7a2-1c2f3e4d5a6b")
	if err != nil || len(got) != 0 || len(captured) != 0 {
		t.Fatalf("got %v, %v after %d queries", got, err, len(captured))
	}
}

func TestGetLastSendTimesBatch_AMalformedIDHasNoSendAndKeepsTheBatchGuarded(t *testing.T) {
	var captured []string
	db, mock := newCapturingDB(t, &captured)
	ids := leadIDs(2)
	sentAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("lead_campaign_sends").
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "last_sent_at"}).AddRow(ids[0], sentAt).AddRow(ids[1], sentAt))

	got, err := NewRepository(db).GetLastSendTimesBatch([]string{ids[0], "not-a-uuid", ids[1]}, "7f9c2ba4-e88f-4d0b-a7a2-1c2f3e4d5a6b")
	if err != nil {
		t.Fatalf("one malformed id must not drop the guard for the whole batch: %v", err)
	}
	if len(got) != 2 || !got[ids[0]].Equal(sentAt) || !got[ids[1]].Equal(sentAt) {
		t.Fatalf("every valid lead keeps its last send, got %v", got)
	}
}
