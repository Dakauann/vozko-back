package opportunity_repository

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/opportunity"
)

// oppColumns is the full opportunities row projection the repository reads.
var oppColumns = []string{
	"id", "workspace_id", "lead_id", "pipeline_id", "stage_id", "owner_id", "carteira_id",
	"title", "value_cents", "currency", "status", "lost_reason_id", "source", "close_date",
	"custom_fields", "created_at", "updated_at", "deleted_at",
}

func stageFilter(stageID string) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{stageID}},
	}}}}
}

// TestSearchByFilter_StageAndValueSort proves the filter-driven read aliases the
// table "o", scopes to the workspace, compiles the stage predicate into the
// WHERE, counts total matches, and sorts by value_cents when SortField=value.
func TestSearchByFilter_StageAndValueSort(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	now := time.Now()

	mock.ExpectQuery(`SELECT count\(\*\) FROM opportunities o WHERE o\.deleted_at IS NULL AND o\.workspace_id = .* AND \(o\.stage_id = ANY`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	rows := sqlmock.NewRows(oppColumns).
		AddRow("opp1", "ws1", nil, "pipe1", "s1", "u1", nil,
			"Deal A", int64(490000), "BRL", "open", nil, "whatsapp", nil, nil, now, now, nil).
		AddRow("opp2", "ws1", nil, "pipe1", "s1", nil, nil,
			"Deal B", int64(120000), "BRL", "open", nil, "", nil, nil, now, now, nil)

	mock.ExpectQuery(`SELECT \* FROM opportunities o WHERE .*ORDER BY o\.value_cents DESC, o\.id ASC`).
		WillReturnRows(rows)

	out, total, err := repo.SearchByFilter(opportunity.SearchByFilterInput{
		WorkspaceID: "ws1",
		Filter:      stageFilter("s1"),
		SortField:   "value",
		SortOrder:   "desc",
		Page:        1,
		PageSize:    50,
	})
	if err != nil {
		t.Fatalf("SearchByFilter: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(out) != 2 || out[0].ID != "opp1" || out[0].ValueCents != 490000 || out[1].ID != "opp2" {
		t.Fatalf("rows mismatch: %#v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// TestSearchByFilter_EmptyShortCircuits proves a zero count returns no rows
// without issuing the page SELECT.
func TestSearchByFilter_EmptyShortCircuits(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT count\(\*\) FROM opportunities o WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))

	out, total, err := repo.SearchByFilter(opportunity.SearchByFilterInput{
		WorkspaceID: "ws1",
		Filter:      stageFilter("s1"),
	})
	if err != nil {
		t.Fatalf("SearchByFilter: %v", err)
	}
	if total != 0 || len(out) != 0 {
		t.Fatalf("expected empty result, got total=%d rows=%d", total, len(out))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// TestSearchByFilter_WorkspaceRequired guards the money/tenant boundary: no
// workspace id -> no query.
func TestSearchByFilter_WorkspaceRequired(t *testing.T) {
	db, _, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	if _, _, err := repo.SearchByFilter(opportunity.SearchByFilterInput{WorkspaceID: "  "}); err != opportunity.ErrWorkspaceRequired {
		t.Fatalf("expected ErrWorkspaceRequired, got %v", err)
	}
}

// TestSumValueByFilter proves the monetary aggregate sums value_cents over all
// matches (COALESCE to 0), scoped to the workspace and the compiled filter.
func TestSumValueByFilter(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(o\.value_cents\), 0\) FROM opportunities o WHERE o\.deleted_at IS NULL AND o\.workspace_id = .* AND \(o\.stage_id = ANY`).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(610000)))

	sum, err := repo.SumValueByFilter(opportunity.SearchByFilterInput{
		WorkspaceID: "ws1",
		Filter:      stageFilter("s1"),
	})
	if err != nil {
		t.Fatalf("SumValueByFilter: %v", err)
	}
	if sum != 610000 {
		t.Fatalf("sum = %d, want 610000", sum)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

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

func sampleCustomFields() map[string]any {
	return map[string]any{
		"segmento": "enterprise",
		"score":    float64(87),
		"ativo":    true,
	}
}

// TestCustomFieldsMarshalRoundTrip proves the custom_fields map survives the
// jsonb marshal/unmarshal used by the repository, in both directions.
func TestCustomFieldsMarshalRoundTrip(t *testing.T) {
	want := sampleCustomFields()

	raw, err := marshalCustomFields(want)
	if err != nil {
		t.Fatalf("marshalCustomFields: %v", err)
	}
	got, err := unmarshalCustomFields(raw)
	if err != nil {
		t.Fatalf("unmarshalCustomFields: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("custom_fields round-trip mismatch:\n got = %#v\nwant = %#v", got, want)
	}

	// Empty map marshals to a nil (SQL NULL) column, not "{}".
	if raw, _ := marshalCustomFields(nil); raw != nil {
		t.Fatalf("empty custom_fields should marshal to nil, got %#v", raw)
	}
	if m, _ := unmarshalCustomFields(nil); m != nil {
		t.Fatalf("nil jsonb should unmarshal to a nil map, got %#v", m)
	}
}

// TestGetByID_CustomFieldsAndStatus exercises the read path end-to-end: a jsonb
// custom_fields column and a won status round-trip into the domain entity.
func TestGetByID_CustomFieldsAndStatus(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	wantCustom := sampleCustomFields()
	customJSON, err := json.Marshal(wantCustom)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "workspace_id", "lead_id", "pipeline_id", "stage_id", "owner_id", "carteira_id",
		"title", "value_cents", "currency", "status", "lost_reason_id", "source", "close_date",
		"custom_fields", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		"opp1", "ws1", "lead1", "pipe1", "stage1", "u1", nil,
		"Cauan - Plano Pro", int64(490000), "BRL", "won", nil, "whatsapp", nil,
		customJSON, now, now, nil,
	)

	mock.ExpectQuery(`SELECT .* FROM "opportunities"`).WillReturnRows(rows)

	got, err := repo.GetByID("ws1", "opp1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != opportunity.StatusWon {
		t.Fatalf("status mismatch: got %q want won", got.Status)
	}
	if got.ValueCents != 490000 || got.Currency != "BRL" {
		t.Fatalf("value/currency mismatch: %d %q", got.ValueCents, got.Currency)
	}
	if !reflect.DeepEqual(got.CustomFields, wantCustom) {
		t.Fatalf("custom_fields mismatch:\n got = %#v\nwant = %#v", got.CustomFields, wantCustom)
	}
	if got.ID != "opp1" || got.WorkspaceID != "ws1" || got.OwnerID != "u1" {
		t.Fatalf("identity mismatch: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// TestGetByID_NotFound maps the GORM record-not-found to the package ErrNotFound.
func TestGetByID_NotFound(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .* FROM "opportunities"`).WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.GetByID("ws1", "missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestUpdate_StatusTransition proves a status transition (open -> won) issues an
// UPDATE and reports success when a row is affected.
func TestUpdate_StatusTransition(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	closeDate := time.Now()
	o := &opportunity.Opportunity{
		ID:          "opp1",
		WorkspaceID: "ws1",
		PipelineID:  "pipe1",
		StageID:     "stage-won",
		Title:       "Deal",
		ValueCents:  100000,
		Currency:    "BRL",
		Status:      opportunity.StatusWon,
		CloseDate:   &closeDate,
	}

	mock.ExpectExec(`UPDATE "opportunities" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Update(o); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// TestUpdate_NotFound returns ErrNotFound when no row matches.
func TestUpdate_NotFound(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewRepository(db)

	o := &opportunity.Opportunity{
		ID: "missing", WorkspaceID: "ws1", PipelineID: "p1", StageID: "s1",
		Title: "x", Currency: "BRL", Status: opportunity.StatusOpen,
	}
	mock.ExpectExec(`UPDATE "opportunities" SET`).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := repo.Update(o); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
