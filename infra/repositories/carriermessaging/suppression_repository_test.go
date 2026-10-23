package carriermessaging_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriermessaging"
)

var suppressionColumns = []string{"id", "workspace_id", "phone", "source", "message_id", "created_at", "updated_at"}

func TestSuppressionAddIsIdempotent(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     bool
	}{
		{"new", 1, true},
		{"again", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`INSERT INTO "carrier_suppressions" .* ON CONFLICT \("workspace_id","phone"\) DO NOTHING`).
				WithArgs(sqlmock.AnyArg(), "ws1", "5511999990000", "keyword", "m1", sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			got, err := NewSuppressionRepository(db).Add(context.Background(), carriermessaging.Suppression{WorkspaceID: "ws1", Phone: "5511999990000", Source: carriermessaging.SuppressionKeyword, MessageID: "m1"})
			if err != nil || got != tc.want {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestSuppressionAddRefusesAnUnknownSource(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewSuppressionRepository(db).Add(context.Background(), carriermessaging.Suppression{WorkspaceID: "ws1", Phone: "p", Source: "rumour"}); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestSuppressionRemove(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`DELETE FROM "carrier_suppressions" WHERE workspace_id = \$1 AND phone = \$2`).
		WithArgs("ws1", "5511999990000").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewSuppressionRepository(db).Remove(context.Background(), "ws1", "5511999990000"); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestSuppressionIsSuppressed(t *testing.T) {
	cases := []struct {
		count int
		want  bool
	}{{0, false}, {1, true}}
	for _, tc := range cases {
		db, mock := newMockDB(t)
		mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_suppressions" WHERE workspace_id = \$1 AND phone = \$2`).
			WithArgs("ws1", "p").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.count))
		got, err := NewSuppressionRepository(db).IsSuppressed(context.Background(), "ws1", "p")
		if err != nil || got != tc.want {
			t.Fatalf("count %d: got %v %v", tc.count, got, err)
		}
	}
}

func TestSuppressedAmongReturnsOnlySuppressedPhones(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT "phone" FROM "carrier_suppressions" WHERE workspace_id = \$1 AND phone IN \(\$2,\$3,\$4\)`).
		WithArgs("ws1", "a", "b", "c").
		WillReturnRows(sqlmock.NewRows([]string{"phone"}).AddRow("b"))
	got, err := NewSuppressionRepository(db).SuppressedAmong(context.Background(), "ws1", []string{"a", "b", "c"})
	if err != nil || len(got) != 1 || !got["b"] {
		t.Fatalf("got %v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestSuppressedAmongNoPhonesRunsNoQuery(t *testing.T) {
	db, mock := newMockDB(t)
	got, err := NewSuppressionRepository(db).SuppressedAmong(context.Background(), "ws1", nil)
	if err != nil || len(got) != 0 || got == nil {
		t.Fatalf("got %v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestSuppressionListPagesNewestFirst(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_suppressions" WHERE workspace_id = \$1`).WithArgs("ws1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))
	mock.ExpectQuery(`SELECT \* FROM "carrier_suppressions" WHERE workspace_id = \$1 ORDER BY created_at DESC, phone ASC LIMIT \$2 OFFSET \$3`).
		WithArgs("ws1", 10, 10).
		WillReturnRows(sqlmock.NewRows(suppressionColumns).
			AddRow("x1", "ws1", "a", "button", "m1", testNow, testNow).
			AddRow("x2", "ws1", "b", "operator", nil, testNow, testNow))
	got, total, err := NewSuppressionRepository(db).List(context.Background(), "ws1", 10, 10)
	if err != nil || total != 12 || len(got) != 2 || got[0].MessageID != "m1" || got[1].Source != carriermessaging.SuppressionOperator {
		t.Fatalf("got %+v %d %v", got, total, err)
	}
	expectationsMet(t, mock)
}

func TestSuppressionListFailsClosedOnAnUnknownSource(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT \* FROM "carrier_suppressions"`).
		WillReturnRows(sqlmock.NewRows(suppressionColumns).AddRow("x1", "ws1", "a", "rumour", nil, testNow, testNow))
	if _, _, err := NewSuppressionRepository(db).List(context.Background(), "ws1", 10, 0); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("got %v", err)
	}
}

func TestSuppressionRepositoryReturnsErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		run  func(carriermessaging.SuppressionRepository) error
	}{
		{"remove", func(r carriermessaging.SuppressionRepository) error {
			return r.Remove(context.Background(), "ws1", "p")
		}},
		{"is suppressed", func(r carriermessaging.SuppressionRepository) error {
			_, err := r.IsSuppressed(context.Background(), "ws1", "p")
			return err
		}},
		{"among", func(r carriermessaging.SuppressionRepository) error {
			_, err := r.SuppressedAmong(context.Background(), "ws1", []string{"p"})
			return err
		}},
		{"list", func(r carriermessaging.SuppressionRepository) error {
			_, _, err := r.List(context.Background(), "ws1", 1, 0)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(`.*`).WillReturnError(boom)
			mock.ExpectExec(`.*`).WillReturnError(boom)
			mock.MatchExpectationsInOrder(false)
			if err := tc.run(NewSuppressionRepository(db)); !errors.Is(err, boom) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
