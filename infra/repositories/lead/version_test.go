package lead

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

const (
	leadUUID  = "6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01"
	wsUUID    = "0b7e3c1a-9d2f-4e5b-8a6c-1d2e3f4a5b6c"
	actorUUID = "8c2d4e6f-1a3b-4c5d-9e7f-0a1b2c3d4e5f"
)

var leadColumns = []string{"id", "workspace_id", "number", "name", "name_source", "version"}

func leadRow(name, nameSource string, version int64) *sqlmock.Rows {
	var source any
	if nameSource != "" {
		source = nameSource
	}
	return sqlmock.NewRows(leadColumns).AddRow(leadUUID, wsUUID, "5511987654321", name, source, version)
}

func TestFindOrCreate_RefusesIncomingDataWithoutASource(t *testing.T) {
	r := newNilRepo()
	if _, _, err := r.FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Name: "Ana"}); !errors.Is(err, lead.ErrLeadSourceInvalid) {
		t.Fatalf("err = %v, want ErrLeadSourceInvalid", err)
	}
	if _, err := r.FindOrCreateMany(wsUUID, []lead.BulkLeadInput{{Number: "5511987654321"}}); !errors.Is(err, lead.ErrLeadSourceInvalid) {
		t.Fatalf("many err = %v, want ErrLeadSourceInvalid", err)
	}
}

func TestFindOrCreate_InsertsAgainstTheLiveIdentityIndexAndReselectsAfterARace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads" WHERE workspace_id = \$1 AND number IN \(\$2,\$3\)`).
		WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leads" WHERE workspace_id = $1 AND number IS NULL AND (id IN (SELECT lead_id FROM lead_phones WHERE workspace_id = $2 AND number = ANY($3))) AND "leads"."deleted_at" IS NULL LIMIT $4`)).
		WithArgs(wsUUID, wsUUID, sqlmock.AnyArg(), 2).WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectExec(regexp.QuoteMeta(`ON CONFLICT ("workspace_id","number") WHERE deleted_at IS NULL DO NOTHING`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT \* FROM "leads" WHERE workspace_id = \$1 AND number IN \(\$2,\$3\)`).
		WillReturnRows(leadRow("Ana", "channel", 3))

	got, created, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, Name: "Aninha"})
	if err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}
	if created || got.ID != leadUUID || got.Name != "Ana" || got.Version != 3 {
		t.Fatalf("a lost race must return the stored lead untouched, got %+v (created %v)", got, created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreate_AFindWithNothingToMergeWritesNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("Ana Souza", "manual", 2))

	got, _, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, Name: "Aninha"})
	if err != nil || got.Name != "Ana Souza" {
		t.Fatalf("a channel name must not replace a manual one: %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreateMany_InsertsAgainstTheLiveIdentityIndex(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectExec(regexp.QuoteMeta(`ON CONFLICT ("workspace_id","number") WHERE deleted_at IS NULL DO NOTHING`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	got, err := (&repository{db: db}).FindOrCreateMany(wsUUID, []lead.BulkLeadInput{{Source: lead.SourceImport, Number: "5511987654321", Name: "Ana"}})
	if err != nil {
		t.Fatalf("FindOrCreateMany: %v", err)
	}
	if l := got["5511987654321"]; l == nil || l.Name != "Ana" || l.Source != lead.SourceImport || l.Version != 1 {
		t.Fatalf("got %+v", l)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func savedLead() *lead.Lead {
	l := &lead.Lead{ID: leadUUID, WorkspaceID: wsUUID, Number: "5511987654321", Name: "Ana Souza", NameSource: lead.SourceManual, Blocked: true, BlockedAt: time.Now(), Version: 4}
	l.EnsureCollections()
	return l
}

func expectStoredCollections(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_phones" WHERE workspace_id = $1 AND lead_id = $2 ORDER BY position`)).
		WithArgs(wsUUID, leadUUID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_addresses" WHERE workspace_id = $1 AND lead_id = $2`)).
		WithArgs(wsUUID, leadUUID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

func TestSave_ChecksTheVersionBumpsItAndWritesTheEventsInOneTransaction(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(exact(saveSQL)).
		WithArgs(
			"5511987654321", "Ana Souza", "manual", nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, true, sqlmock.AnyArg(), nil, "", nil, sqlmock.AnyArg(),
			leadUUID, wsUUID, int64(4), "5511987654321",
		).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	expectStoredCollections(mock)
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	l := savedLead()
	events := []recordevent.Event{{Actor: actorUUID, Kind: lead.EventBlocked, Changes: []recordevent.Change{{Field: "blocked", Before: false, After: true}}}}
	if err := (&repository{db: db}).Save(context.Background(), l, 4, events); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if l.Version != 5 {
		t.Fatalf("version = %d, want 5", l.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSave_WritesTheOptOutWithItsSource(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	l := savedLead()
	l.Blocked, l.BlockedAt = false, time.Time{}
	optedOut := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	l.OptedOutAt, l.OptOutSource = &optedOut, lead.OptOutLeadRequest

	mock.ExpectBegin()
	mock.ExpectQuery(exact(saveSQL)).
		WithArgs(
			"5511987654321", "Ana Souza", "manual", nil, nil, nil, nil, nil, nil, nil, nil,
			nil, &optedOut, "lead_request", false, nil, nil, "", nil, sqlmock.AnyArg(),
			leadUUID, wsUUID, int64(4), "5511987654321",
		).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	expectStoredCollections(mock)
	mock.ExpectCommit()

	if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSave_TellsAStaleVersionFromAMissingLead(t *testing.T) {
	cases := []struct {
		name    string
		current *sqlmock.Rows
		want    error
	}{
		{"the lead moved on", sqlmock.NewRows([]string{"version"}).AddRow(7), shared.ErrVersionConflict},
		{"the lead is gone", sqlmock.NewRows([]string{"version"}), lead.ErrLeadNotFound},
		{"a conversation started on the old number in the meantime", sqlmock.NewRows([]string{"version"}).AddRow(4), lead.ErrIdentityInUse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(`UPDATE leads SET`).WillReturnRows(sqlmock.NewRows([]string{"version"}))
			mock.ExpectQuery(`SELECT version FROM leads WHERE id = \$1 AND workspace_id = \$2 AND deleted_at IS NULL`).
				WithArgs(leadUUID, wsUUID).WillReturnRows(tc.current)
			mock.ExpectRollback()

			err := (&repository{db: db}).Save(context.Background(), savedLead(), 4, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSave_RefusesWithoutTheReadVersion(t *testing.T) {
	if err := newNilRepo().Save(context.Background(), savedLead(), 0, nil); !errors.Is(err, shared.ErrVersionRequired) {
		t.Fatalf("err = %v, want ErrVersionRequired", err)
	}
}

func TestInsert_RefusesAnIdentityThatIsAlreadyTaken(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`ON CONFLICT ("workspace_id","number") WHERE deleted_at IS NULL DO NOTHING`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	l := &lead.Lead{WorkspaceID: wsUUID, Number: "5511987654321", Name: "Ana", Source: lead.SourceManual}
	if err := (&repository{db: db}).Insert(context.Background(), l, nil); !errors.Is(err, lead.ErrLeadDuplicate) {
		t.Fatalf("err = %v, want ErrLeadDuplicate", err)
	}
}

func TestInsert_ALeadWithoutIdentitySkipsTheNumberLookup(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "leads"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	l := &lead.Lead{WorkspaceID: wsUUID, Name: "Maria", Source: lead.SourceManual}
	events := []recordevent.Event{{Actor: actorUUID, Kind: lead.EventCreated, Changes: []recordevent.Change{{Field: "name", After: "Maria"}}}}
	if err := (&repository{db: db}).Insert(context.Background(), l, events); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if l.ID == "" || l.Version != 1 {
		t.Fatalf("an inserted lead gets an id and version 1: %+v", l)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaMapping_AnEmptyIdentityIsStoredAsNull(t *testing.T) {
	row, err := toSchema(&lead.Lead{WorkspaceID: wsUUID, Name: "Maria"})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := row.Number.Value(); err != nil || value != nil {
		t.Fatalf("an empty number must be stored as NULL, got %v (%v)", value, err)
	}
	var scanned schema.OptionalText
	if err := scanned.Scan(nil); err != nil || scanned != "" {
		t.Fatalf("NULL must map back to an empty number, got %q (%v)", scanned, err)
	}
	if back, err := toDomain(row); err != nil || back.Number != "" {
		t.Fatalf("number = %+v, %v", back, err)
	}
}

func TestSchemaMapping_CarriesTheRecordFields(t *testing.T) {
	birth := shared.Date{Year: 1990, Month: time.April, Day: 21}
	optedOut := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	src := &lead.Lead{
		ID: leadUUID, WorkspaceID: wsUUID, Number: "5511987654321", Name: "Ana", NameSource: lead.SourceManual,
		Nickname: "Aninha", Email: "ana@x.com", BirthDate: &birth, Source: lead.SourceImport, Owner: "ai:" + actorUUID,
		CustomFields:  map[string]any{"cor": "azul"},
		WhatsAppOptIn: &lead.Consent{GrantedAt: optedOut, Source: lead.ConsentImport}, OptedOutAt: &optedOut,
		RelativesCount: 2, ReferredCount: 3, Version: 9,
	}
	row, err := toSchema(src)
	if err != nil {
		t.Fatal(err)
	}
	if row.OwnerID != actorUUID || row.OwnerKind != "ai" || row.BirthDate != "1990-04-21" {
		t.Fatalf("owner split = %q %q, birth date %q", row.OwnerID, row.OwnerKind, row.BirthDate)
	}
	back, err := toDomain(row)
	if err != nil {
		t.Fatal(err)
	}
	if back.Owner != src.Owner || back.Nickname != "Aninha" || back.Email != "ana@x.com" || back.BirthDate == nil || *back.BirthDate != birth ||
		back.NameSource != lead.SourceManual || back.Source != lead.SourceImport || back.CustomFields["cor"] != "azul" ||
		back.WhatsAppOptIn == nil || back.WhatsAppOptIn.Source != lead.ConsentImport || back.OptedOutAt == nil ||
		back.RelativesCount != 2 || back.ReferredCount != 3 || back.Version != 9 {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestEntryRefs_ReadsTheLeadEntriesOfEveryChannelInsideItsWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM \(SELECT wce_e\.lead_id AS lead_id.* WHERE lead_entries\.lead_id = \$1 AND EXISTS \(SELECT 1 FROM leads WHERE leads\.id = \$2 AND leads\.workspace_id = \$3\)`).
		WithArgs(leadUUID, leadUUID, wsUUID).
		WillReturnRows(sqlmock.NewRows([]string{"entry_id", "entry_type"}).AddRow("e-1", "whatsapp").AddRow("c-1", "telegram"))

	refs, err := (&repository{db: db}).EntryRefs(context.Background(), wsUUID, leadUUID)
	if err != nil {
		t.Fatalf("EntryRefs: %v", err)
	}
	if len(refs) != 2 || refs[1].EntryType != shared.EntryTypeTelegram {
		t.Fatalf("refs = %+v", refs)
	}
	if _, err := newNilRepo().EntryRefs(context.Background(), "", leadUUID); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("a workspace is required, got %v", err)
	}
}

func TestDelete_ASoftDeleteBumpsTheVersionToo(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE leads SET deleted_at = \$1, version = version \+ 1 WHERE id = \$2 AND workspace_id = \$3 AND deleted_at IS NULL`).
		WithArgs(sqlmock.AnyArg(), leadUUID, wsUUID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := (&repository{db: db}).Delete(wsUUID, leadUUID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSave_ALegacyNumberDoesNotStopATargetedChange(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE leads SET`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	expectStoredCollections(mock)
	mock.ExpectCommit()

	l := savedLead()
	l.Number = "11987654"
	if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
		t.Fatalf("a lead whose stored number predates the parser must still be blockable, got %v", err)
	}
}

func TestReading_UnreadableCustomFieldsRefuseInsteadOfBeingErasedLater(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "number", "custom_fields", "version"}).
		AddRow(leadUUID, wsUUID, "5511987654321", `["not","an","object"]`, 3))

	if _, err := (&repository{db: db}).FindByID(wsUUID, leadUUID); err == nil {
		t.Fatal("custom fields that are not an object must refuse the read, or a later save would erase them")
	}
}

func TestSaveSQLGuardsTheIdentityAgainstAConversationInTheSameStatement(t *testing.T) {
	for _, want := range []string{
		"WHERE id = ? AND workspace_id = ? AND version = ? AND deleted_at IS NULL",
		"AND (leads.number IS NOT DISTINCT FROM ? OR NOT EXISTS (SELECT 1 FROM ",
		"WHERE lead_entries.lead_id = leads.id)) RETURNING version",
	} {
		if !strings.Contains(saveSQL, want) {
			t.Errorf("saveSQL lacks %q:\n%s", want, saveSQL)
		}
	}
	if strings.Contains(saveSQL, "relatives_count") || strings.Contains(saveSQL, "referred_count") {
		t.Error("the relation counts move only with the relations, never with a record save")
	}
}
