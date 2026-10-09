package lead

import (
	"database/sql/driver"
	"fmt"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var placeholderPattern = regexp.MustCompile(`\$\d+`)

type capturedQuery struct {
	sql string
}

func newCapturingDB(t *testing.T, captured *[]capturedQuery) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		*captured = append(*captured, capturedQuery{sql: actual})
		return nil
	})
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func manyLeadIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprint(i))).String()
	}
	return ids
}

type argCounter struct{ total *int }

func (a argCounter) Match(driver.Value) bool {
	*a.total++
	return true
}

func TestFindByIDs_BindsOneArrayForSeventyThousandIDs(t *testing.T) {
	var captured []capturedQuery
	db, mock := newCapturingDB(t, &captured)
	var args int
	mock.ExpectQuery("leads").
		WithArgs(argCounter{&args}, argCounter{&args}).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "number"}).AddRow(manyLeadIDs(1)[0], "ws-1", "5584994409624"))

	leads, err := (&repository{db: db}).FindByIDs("ws-1", manyLeadIDs(70_000))
	if err != nil {
		t.Fatal(err)
	}
	if len(leads) != 1 {
		t.Fatalf("got %d leads", len(leads))
	}
	if len(captured) != 1 {
		t.Fatalf("want one query, got %d", len(captured))
	}
	if got := len(placeholderPattern.FindAllString(captured[0].sql, -1)); got > 3 {
		t.Fatalf("%d placeholders for 70,000 ids, want the ids bound as one array:\n%.300s", got, captured[0].sql)
	}
	if !regexp.MustCompile(`id = ANY\(\$\d+::uuid\[\]\)`).MatchString(captured[0].sql) {
		t.Fatalf("ids must bind as one uuid array:\n%s", captured[0].sql)
	}
	if args != 2 {
		t.Fatalf("want workspace and the id array bound, got %d args", args)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByIDs_DropsMalformedAndDuplicateIDsBeforeQuerying(t *testing.T) {
	var captured []capturedQuery
	db, _ := newCapturingDB(t, &captured)

	got, err := (&repository{db: db}).FindByIDs("ws-1", []string{"not-a-uuid", " ", "'; drop table leads; --"})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if len(captured) != 0 {
		t.Fatal("ids that cannot exist must not reach the database")
	}
}
