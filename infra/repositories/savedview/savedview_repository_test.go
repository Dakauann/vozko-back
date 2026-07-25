package savedview_repository

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
		WithoutReturning:     true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func sampleFilter() crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{"s1", "s2"}},
			{Field: crmfilter.FieldValue, Operator: crmfilter.OpGreaterEq, Values: []string{"1000"}},
		},
	}}}
}

// TestFilterMarshalUnmarshalRoundTrip proves the crmfilter.Filter survives the
// jsonb marshal/unmarshal used by the repository, in both directions.
func TestFilterMarshalUnmarshalRoundTrip(t *testing.T) {
	want := sampleFilter()

	raw, err := marshalFilter(want)
	if err != nil {
		t.Fatalf("marshalFilter: %v", err)
	}
	got, err := unmarshalFilter(raw)
	if err != nil {
		t.Fatalf("unmarshalFilter: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter round-trip mismatch:\n got = %#v\nwant = %#v", got, want)
	}
}

// TestGetByID_FilterRoundTrip exercises the read path end-to-end: a jsonb
// filter column stored in the DB is unmarshaled back into the domain Filter.
func TestGetByID_FilterRoundTrip(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	want := sampleFilter()
	filterJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	columnsJSON := []byte(`["name","value"]`)

	rows := sqlmock.NewRows([]string{
		"id", "workspace_id", "owner_id", "object_type", "pipeline_id", "name",
		"filter", "group_by", "group_by_key", "sort_field", "sort_dir", "columns",
		"visibility", "is_default", "position",
	}).AddRow(
		"v1", "ws1", "u1", "conversation", nil, "Minhas abertas",
		filterJSON, "stage", "", "", "desc", columnsJSON,
		"private", false, 1,
	)

	mock.ExpectQuery(`SELECT .* FROM "saved_views"`).WillReturnRows(rows)

	got, err := repo.GetByID("ws1", "v1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !reflect.DeepEqual(got.Filter, want) {
		t.Fatalf("filter mismatch:\n got = %#v\nwant = %#v", got.Filter, want)
	}
	if !reflect.DeepEqual(got.Columns, []string{"name", "value"}) {
		t.Fatalf("columns mismatch: got %#v", got.Columns)
	}
	if got.ID != "v1" || got.WorkspaceID != "ws1" || got.OwnerID != "u1" {
		t.Fatalf("identity mismatch: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
