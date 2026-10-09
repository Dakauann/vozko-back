package opportunity_repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/opportunity"
	crmfiltersql "vozko/infra/repositories/crmfilter"
)

func customFilter(p crmfilter.Predicate) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{p}}}}
}

func TestSearchByFilterBindsEveryCustomPlaceholder(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	score := crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "score", Operator: crmfilter.OpGreaterEq, Values: []string{"10"}}.
		BindKind(crmfilter.KindNumber)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(o\.value_cents\), 0\) FROM opportunities o WHERE o\.deleted_at IS NULL AND o\.workspace_id = \$1 AND \(\(CASE WHEN pg_input_is_valid\(o\.custom_fields ->> \$2::text, 'numeric'\) THEN \(o\.custom_fields ->> \$3::text\)::numeric END\) >= \$4::numeric\)`).
		WithArgs("ws1", "score", "score", float64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(0)))

	if _, err := NewRepository(db).SumValueByFilter(opportunity.SearchByFilterInput{WorkspaceID: "ws1", Filter: customFilter(score)}); err != nil {
		t.Fatalf("SumValueByFilter: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestSearchByFilterKeepsContainmentOperatorsIntact(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	tags := crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "tags", Operator: crmfilter.OpIn, Values: []string{"x", "y"}}.
		BindKind(crmfilter.KindMultiEnum)

	mock.ExpectQuery(`SELECT count\(\*\) FROM opportunities o WHERE o\.deleted_at IS NULL AND o\.workspace_id = \$1 AND \(o\.custom_fields @> ANY\(\$2::jsonb\[\]\)\)`).
		WithArgs("ws1", pq.Array([]string{`{"tags":["x"]}`, `{"tags":["y"]}`})).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))

	if _, _, err := NewRepository(db).SearchByFilter(opportunity.SearchByFilterInput{WorkspaceID: "ws1", Filter: customFilter(tags)}); err != nil {
		t.Fatalf("SearchByFilter: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestSearchByFilterRefusesAnUnboundCustomPredicate(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	raw := crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "score", Operator: crmfilter.OpEquals, Values: []string{"1"}}
	_, _, err := NewRepository(db).SearchByFilter(opportunity.SearchByFilterInput{WorkspaceID: "ws1", Filter: customFilter(raw)})
	if !errors.Is(err, crmfiltersql.ErrUnboundCustomField) {
		t.Fatalf("SearchByFilter() error = %v, want ErrUnboundCustomField", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a query ran for an unbound predicate: %v", err)
	}
}
