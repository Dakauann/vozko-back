package lead

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

const mergePattern = `UPDATE leads SET name = v\.name, name_source = NULLIF\(v\.name_source, ''\), profile_picture_url = v\.profile_picture_url, age = v\.age, updated_at = \$1, version = leads\.version \+ 1 FROM unnest\(\$2::uuid\[\], \$3::bigint\[\], \$4::text\[\], \$5::text\[\], \$6::text\[\], \$7::int\[\]\) AS v\(id, version, name, name_source, profile_picture_url, age\) WHERE leads\.id = v\.id AND leads\.version = v\.version AND leads\.workspace_id = \$8 AND leads\.deleted_at IS NULL RETURNING leads\.id::text AS id, leads\.version AS version$`

const reloadPattern = `SELECT \* FROM "leads" WHERE workspace_id = \$1 AND id = ANY\(\$2::uuid\[\]\) AND "leads"\."deleted_at" IS NULL`

func mergedRows(pairs ...interface{}) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "version"})
	for i := 0; i < len(pairs); i += 2 {
		rows.AddRow(pairs[i], pairs[i+1])
	}
	return rows
}

func mergeArgs(ids, versions, names, sources string) []driver.Value {
	return []driver.Value{sqlmock.AnyArg(), ids, versions, names, sources, sqlmock.AnyArg(), sqlmock.AnyArg(), wsUUID}
}

func anyArgs(n int) []driver.Value {
	args := make([]driver.Value, n)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	return args
}

func TestFindOrCreate_TheMergeIsGuardedByTheVersionThatWasRead(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("Aninha", "channel", 4))
	mock.ExpectQuery(mergePattern).
		WithArgs(mergeArgs(`{"`+leadUUID+`"}`, "{4}", `{"Ana Souza"}`, `{"import"}`)...).
		WillReturnRows(mergedRows(leadUUID, 5))

	got, created, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if err != nil || created {
		t.Fatalf("FindOrCreate: %v (created %v)", err, created)
	}
	if got.Name != "Ana Souza" || got.NameSource != lead.SourceImport || got.Version != 5 {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreate_AManualRenameBetweenTheReadAndTheWriteSurvives(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("Aninha", "channel", 4))
	mock.ExpectQuery(mergePattern).WithArgs(anyArgs(8)...).WillReturnRows(mergedRows())
	mock.ExpectQuery(reloadPattern).
		WithArgs(wsUUID, `{"`+leadUUID+`"}`).
		WillReturnRows(leadRow("Ana Maria", "manual", 5))

	got, _, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}
	if got.Name != "Ana Maria" || got.NameSource != lead.SourceManual || got.Version != 5 {
		t.Fatalf("the manual rename must survive the import, got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreate_AMergeThatKeepsLosingAnswersAConflict(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("Aninha", "channel", 4))
	for version := int64(5); version <= 6; version++ {
		mock.ExpectQuery(mergePattern).WithArgs(anyArgs(8)...).WillReturnRows(mergedRows())
		mock.ExpectQuery(reloadPattern).WillReturnRows(leadRow("Aninha", "channel", version))
	}
	mock.ExpectQuery(mergePattern).WithArgs(anyArgs(8)...).WillReturnRows(mergedRows())

	_, _, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("err = %v, want a version conflict after the last attempt", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreate_ALeadDeletedBeforeTheMergeIsNotFound(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("Aninha", "channel", 4))
	mock.ExpectQuery(mergePattern).WithArgs(anyArgs(8)...).WillReturnRows(mergedRows())
	mock.ExpectQuery(reloadPattern).WillReturnRows(sqlmock.NewRows(leadColumns))

	_, _, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("err = %v, want ErrLeadNotFound", err)
	}
}

func manyLeads(n int) ([]lead.BulkLeadInput, *sqlmock.Rows, []string) {
	inputs := make([]lead.BulkLeadInput, n)
	rows := sqlmock.NewRows(leadColumns)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		number := fmt.Sprintf("551198%07d", i)
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		inputs[i] = lead.BulkLeadInput{Source: lead.SourceImport, Number: number, Name: fmt.Sprintf("Pessoa %d", i)}
		rows.AddRow(ids[i], wsUUID, number, "", nil, 1)
	}
	return inputs, rows, ids
}

func TestFindOrCreateMany_WritesTheChangedLeadsInOneStatementPerFiveHundred(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	inputs, rows, ids := manyLeads(1200)
	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(rows)
	for i := 0; i < 4; i++ {
		mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(sqlmock.NewRows(leadColumns))
	}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		returned := mergedRows()
		for _, id := range ids[start:end] {
			returned.AddRow(id, 2)
		}
		mock.ExpectQuery(mergePattern).WithArgs(anyArgs(8)...).WillReturnRows(returned)
	}

	got, err := (&repository{db: db}).FindOrCreateMany(wsUUID, inputs)
	if err != nil {
		t.Fatalf("FindOrCreateMany: %v", err)
	}
	if len(got) != 1200 || got[inputs[1199].Number].Version != 2 || got[inputs[0].Number].Name != "Pessoa 0" {
		t.Fatalf("got %d leads, last %+v", len(got), got[inputs[1199].Number])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreateMany_AFailedMergeFailsTheCall(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("", "", 1))
	mock.ExpectQuery(mergePattern).WithArgs(anyArgs(8)...).WillReturnError(errors.New("connection reset"))

	_, err := (&repository{db: db}).FindOrCreateMany(wsUUID, []lead.BulkLeadInput{{Source: lead.SourceImport, Number: "5511987654321", Name: "Ana"}})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("a failed merge must fail the call, got %v", err)
	}
}

func TestFindOrCreateMany_ALeadCreatedByARacingCallStillReceivesTheIncomingName(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	const ours = "11111111-1111-4111-8111-111111111111"
	const theirs = "22222222-2222-4222-8222-222222222222"
	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectExec(regexp.QuoteMeta(`ON CONFLICT ("workspace_id","number") WHERE deleted_at IS NULL DO NOTHING`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(sqlmock.NewRows(leadColumns).
		AddRow(ours, wsUUID, "5511987654321", "Ana", "import", 1).
		AddRow(theirs, wsUUID, "5511912345678", "Bia", "channel", 1))
	mock.ExpectQuery(mergePattern).
		WithArgs(mergeArgs(`{"`+theirs+`"}`, "{1}", `{"Beatriz Lima"}`, `{"import"}`)...).
		WillReturnRows(mergedRows(theirs, 2))

	repo := &repository{db: db, newID: func() string { return ours }}
	got, err := repo.FindOrCreateMany(wsUUID, []lead.BulkLeadInput{
		{Source: lead.SourceImport, Number: "5511987654321", Name: "Ana"},
		{Source: lead.SourceImport, Number: "5511912345678", Name: "Beatriz Lima"},
	})
	if err != nil {
		t.Fatalf("FindOrCreateMany: %v", err)
	}
	if raced := got["5511912345678"]; raced == nil || raced.ID != theirs || raced.Name != "Beatriz Lima" || raced.Version != 2 {
		t.Fatalf("the raced lead must get the imported name, got %+v", raced)
	}
	if mine := got["5511987654321"]; mine == nil || mine.ID != ours {
		t.Fatalf("our insert must come back as created, got %+v", mine)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
