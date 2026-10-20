package database

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type findByIDsRow struct {
	ID   string
	Name string
}

func (findByIDsRow) TableName() string { return "find_by_ids_rows" }

func TestFindByIDsRunsOneQueryForDistinctIDs(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "find_by_ids_rows" WHERE id IN \(\$1,\$2\)`).
		WithArgs("a", "b").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("a", "first").AddRow("b", "second"))

	rows, err := FindByIDs[findByIDsRow](context.Background(), db, []string{"a", " b ", "a", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "first" || rows[1].Name != "second" {
		t.Fatalf("rows = %+v", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByIDsSkipsTheQueryWithoutIDs(t *testing.T) {
	db, mock, _ := sqlmockDB(t)

	rows, err := FindByIDs[findByIDsRow](context.Background(), db, []string{"", "  "})
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByIDsReturnsTheQueryError(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	boom := errors.New("boom")
	mock.ExpectQuery(`SELECT \* FROM "find_by_ids_rows"`).WillReturnError(boom)

	if _, err := FindByIDs[findByIDsRow](context.Background(), db, []string{"a"}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the query error", err)
	}
}

func TestFindByIDsChunksLongLists(t *testing.T) {
	db, mock, _ := sqlmockDB(t)
	ids := make([]string, maxIDsPerFind+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%d", i)
	}
	mock.ExpectQuery(`SELECT \* FROM "find_by_ids_rows"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM "find_by_ids_rows"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := FindByIDs[findByIDsRow](context.Background(), db, ids); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
