package lead

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/address"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

const (
	otherUUID   = "1d2c3b4a-5f6e-4d7c-8b9a-0f1e2d3c4b5a"
	phoneUUID   = "2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d"
	addressUUID = "4b5c6d7e-8f90-4a1b-8c2d-3e4f5a6b7c8d"

	nextRelationUUID = "7d8e9f00-1a2b-4c3d-8e4f-5a6b7c8d9e0f"
)

func TestLoadReadsThePhonesAndAddressesWithOneArrayBoundQueryEach(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	fixedAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT \* FROM "leads"`).WillReturnRows(leadRow("Maria", "manual", 4))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_phones" WHERE workspace_id = $1 AND lead_id = ANY($2::uuid[]) ORDER BY lead_id, position`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "number", "label", "position"}).
			AddRow(phoneUUID, wsUUID, leadUUID, "551133334444", "landline", 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_addresses" WHERE workspace_id = $1 AND lead_id = ANY($2::uuid[]) ORDER BY lead_id, position`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "label", "is_primary", "zip_code", "city", "state", "latitude", "longitude", "geo_precision", "geo_source", "geo_status", "geocoded_at"}).
			AddRow("a-1", leadUUID, "home", true, "01310100", "São Paulo", "SP", -23.56, -46.65, "postal_code", "reference", "approximate", fixedAt))

	got, err := (&repository{db: db}).Load(context.Background(), wsUUID, leadUUID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.IsAggregate() || len(got.Phones) != 1 || got.Phones[0].Label != lead.PhoneLandline {
		t.Fatalf("phones = %+v", got.Phones)
	}
	a := got.Addresses[0]
	if !a.Primary || a.GeoStatus != lead.GeoApproximate || a.Fix == nil || a.Fix.Point.Lat != -23.56 || !a.Fix.FixedAt.Equal(fixedAt) {
		t.Fatalf("address = %+v", a)
	}
	if got.Relations != nil {
		t.Fatalf("a record edit never reads the relations, got %+v", got.Relations)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByIDAnswersNotFoundForAMalformedIDWithoutAQuery(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if _, err := (&repository{db: db}).FindByID(wsUUID, "abc"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("FindByID = %v, want not found", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRefusesALeadReadWithoutItsLists(t *testing.T) {
	record := &lead.Lead{ID: leadUUID, WorkspaceID: wsUUID, Name: "Maria", Version: 2}
	if err := newNilRepo().Save(context.Background(), record, 2, nil); !errors.Is(err, lead.ErrAggregateNotLoaded) {
		t.Fatalf("Save = %v, want %v: saving a bare record would erase its phones and addresses", err, lead.ErrAggregateNotLoaded)
	}
}

func TestAddRelationLocksBothLeadsInOrderAndCountsAndRecordsBothSides(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	relationID := "3c4d5e6f-7a8b-4c9d-8e0f-1a2b3c4d5e6f"

	mock.ExpectBegin()
	mock.ExpectQuery(exact(lockLeadsSQL)).WithArgs(wsUUID, pq.StringArray{otherUUID, leadUUID}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(otherUUID).AddRow(leadUUID))
	mock.ExpectExec(`INSERT INTO "lead_relations"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(exact(shiftRelationCountsSQL)).
		WithArgs(sqlmock.AnyArg(), pq.StringArray{otherUUID, leadUUID}, pq.Int64Array{0, 0}, pq.Int64Array{0, 1}, wsUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "relatives_count", "referred_count"}).
			AddRow(otherUUID, 2, 0, 0).AddRow(leadUUID, 5, 0, 1))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WithArgs(append(eventValues(leadUUID), eventValues(otherUUID)...)...).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	repo := &repository{db: db, newID: func() string { return relationID }}
	r, err := lead.NewRelation(leadUUID, otherUUID, lead.KindReferred, actorUUID)
	if err != nil {
		t.Fatal(err)
	}
	written, err := repo.AddRelation(context.Background(), wsUUID, r)
	if err != nil {
		t.Fatalf("AddRelation: %v", err)
	}
	if written.Relation.ID != relationID || written.Relation.CreatedAt.IsZero() || written.Leads[leadUUID].Version != 5 || written.Leads[leadUUID].Counts.Referred != 1 {
		t.Fatalf("written = %+v", written)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAddRelationRefusesALeadThatIsGoneAndAKindThatIsNotStored(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(exact(lockLeadsSQL)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(leadUUID))
	mock.ExpectRollback()

	repo := &repository{db: db}
	if _, err := repo.AddRelation(context.Background(), wsUUID, lead.Relation{LeadID: leadUUID, OtherLeadID: otherUUID, Kind: lead.KindSibling, CreatedBy: actorUUID}); !errors.Is(err, lead.ErrRelativeNotFound) {
		t.Fatalf("AddRelation = %v, want %v", err, lead.ErrRelativeNotFound)
	}
	if _, err := repo.AddRelation(context.Background(), wsUUID, lead.Relation{LeadID: leadUUID, OtherLeadID: otherUUID, Kind: lead.KindParent}); !errors.Is(err, lead.ErrRelationKindInvalid) {
		t.Fatalf("a kind in the stored direction only = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveRelationDeletesWhateverSideIsLiveAndAnswersNotFoundOtherwise(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(exact(findRelationSQL)).WithArgs(wsUUID, relationUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "other_lead_id", "dimension", "kind"}).
			AddRow(relationUUID, wsUUID, otherUUID, leadUUID, "family", "child"))
	mock.ExpectQuery(exact(lockLeadsSQL)).WithArgs(wsUUID, pq.StringArray{otherUUID, leadUUID}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(leadUUID))
	mock.ExpectQuery(exact(deleteRelationSQL)).WithArgs(wsUUID, relationUUID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(relationUUID))
	mock.ExpectQuery(exact(shiftRelationCountsSQL)).
		WithArgs(sqlmock.AnyArg(), pq.StringArray{otherUUID, leadUUID}, pq.Int64Array{-1, -1}, pq.Int64Array{0, 0}, wsUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "relatives_count", "referred_count"}).AddRow(leadUUID, 7, 0, 0))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	repo := &repository{db: db}
	written, err := repo.RemoveRelation(context.Background(), wsUUID, relationUUID, actorUUID)
	if err != nil {
		t.Fatalf("RemoveRelation: %v", err)
	}
	if len(written.Leads) != 1 || written.Leads[leadUUID].Version != 7 || written.Relation.LeadID != otherUUID {
		t.Fatalf("written = %+v", written)
	}
	if _, err := repo.RemoveRelation(context.Background(), wsUUID, "not-a-uuid", actorUUID); !errors.Is(err, lead.ErrRelationNotFound) {
		t.Fatalf("a malformed id = %v, want not found without a query", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListRelativesPagesByKeysetOverBothSides(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)
	after := lead.RelativesCursor{CreatedAt: at, RelationID: relationUUID}
	q := lead.RelativesQuery{LeadID: leadUUID, Dimension: lead.DimensionReferral, After: after.Encode(), Limit: 1}

	sql, args := relativesQuery(wsUUID, q, &after)
	for _, want := range []string{
		"(SELECT r.id, r.lead_id, r.other_lead_id, r.kind, r.created_by, r.created_at, r.other_lead_id AS relative_id FROM lead_relations r WHERE r.workspace_id = ? AND r.lead_id = ? AND r.dimension = ? AND (r.created_at, r.id) > (?::timestamptz, ?::uuid) AND EXISTS (SELECT 1 FROM leads o WHERE o.id = r.other_lead_id AND o.deleted_at IS NULL) ORDER BY r.created_at, r.id LIMIT ?)",
		" UNION ALL (SELECT r.id, r.lead_id, r.other_lead_id, r.kind, r.created_by, r.created_at, r.lead_id AS relative_id FROM lead_relations r WHERE r.workspace_id = ? AND r.other_lead_id = ?",
		") p JOIN leads o ON o.id = p.relative_id ORDER BY p.created_at, p.id LIMIT ?",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("relatives SQL lacks %q:\n%s", want, sql)
		}
	}
	if strings.Count(sql, "?") != len(args) || len(args) != 13 {
		t.Fatalf("%d placeholders for %d arguments", strings.Count(sql, "?"), len(args))
	}

	mock.ExpectQuery(exact(sql)).WithArgs(wsUUID, leadUUID, "referral", at, relationUUID, 2, wsUUID, leadUUID, "referral", at, relationUUID, 2, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "other_lead_id", "kind", "created_at", "relative_id", "relative_name", "relative_number"}).
			AddRow(nextRelationUUID, leadUUID, otherUUID, "referred", at.Add(time.Minute), otherUUID, "Pedro", "5511912345678").
			AddRow(addressUUID, leadUUID, phoneUUID, "referred", at.Add(2*time.Minute), phoneUUID, "Ana", nil))
	page, err := (&repository{db: db}).ListRelatives(context.Background(), wsUUID, q)
	if err != nil {
		t.Fatalf("ListRelatives: %v", err)
	}
	if len(page.Relatives) != 1 || page.Relatives[0].Lead.Name != "Pedro" || page.Next == "" {
		t.Fatalf("page = %+v", page)
	}
	next, _, _ := lead.RelativesQuery{LeadID: leadUUID, After: page.Next}.Cursor()
	if next.RelationID != nextRelationUUID || !next.CreatedAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("next = %+v", next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRewritesThePhonesOnlyWhenTheyChanged(t *testing.T) {
	stored := sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "number", "label", "position"}).
		AddRow(phoneUUID, wsUUID, leadUUID, "551133334444", "landline", 0)
	cases := []struct {
		name    string
		label   lead.PhoneLabel
		rewrite bool
	}{
		{"the same phones write nothing", lead.PhoneLandline, false},
		{"a new label rewrites the list", lead.PhoneWork, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
			mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(stored)
			if tc.rewrite {
				mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM lead_phones WHERE workspace_id = $1 AND lead_id = $2`)).
					WithArgs(wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO "lead_phones"`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectCommit()

			l := savedLead()
			l.Phones = []lead.ContactPhone{{ID: phoneUUID, Number: "551133334444", Label: tc.label}}
			if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func storedAddressRow(zip string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "label", "is_primary", "position", "zip_code", "street", "number", "district", "city", "state", "geo_status", "fingerprint"}).
		AddRow(addressUUID, wsUUID, leadUUID, "home", true, 0, zip, "Avenida Paulista", "1000", "Bela Vista", "São Paulo", "SP", "approximate", "f-old")
}

func TestSaveRequeuesAnEditedAddressAndBindsTheQueueTimeBeforeTheKeys(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(storedAddressRow("01310100"))
	args := append(anyArgs(22), sqlmock.AnyArg(), addressUUID, leadUUID, wsUUID)
	args[0], args[1], args[18] = "home", true, "pending"
	mock.ExpectExec(exact(requeueAddressSQL)).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
	expectRelease(mock, pq.StringArray{"f-old"})
	mock.ExpectCommit()

	l := savedLead()
	l.Addresses = []lead.Address{{ID: addressUUID, Label: lead.AddressHome, Primary: true, GeoStatus: lead.GeoApproximate,
		Postal: paulistaPostal()}}
	if err := l.SetAddresses([]lead.AddressInput{{ID: addressUUID, Label: lead.AddressHome, Primary: true, Postal: paulistaPostalAt("01311000")}}); err != nil {
		t.Fatal(err)
	}
	if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveWritesNothingForAnAddressThatDidNotChange(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	l := savedLead()
	l.Addresses = []lead.Address{{ID: addressUUID, Label: lead.AddressHome, Primary: true, GeoStatus: lead.GeoApproximate, Postal: paulistaPostal()}}
	unchanged := addressRow(l, 0, l.Addresses[0], time.Now())

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "label", "is_primary", "position", "zip_code", "street", "number", "district", "district_key", "city", "city_key", "state", "geo_status", "fingerprint"}).
			AddRow(addressUUID, wsUUID, leadUUID, "home", true, 0, string(unchanged.ZipCode), string(unchanged.Street), string(unchanged.Number), string(unchanged.District),
				string(unchanged.DistrictKey), string(unchanged.City), string(unchanged.CityKey), string(unchanged.State), unchanged.GeoStatus, unchanged.Fingerprint))
	mock.ExpectCommit()

	if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByNumbersSearchesTheContactPhonesAsASemiJoinWithTheIdentityFirst(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	formats := pq.StringArray{"5511987654321", "551187654321", "551133334444"}

	mock.ExpectQuery(exact(holdingNumbersSQL)).
		WithArgs(wsUUID, wsUUID, formats, wsUUID, formats, formats, directoryLookupLimit).
		WillReturnRows(sqlmock.NewRows(leadColumns).
			AddRow(leadUUID, wsUUID, "5511987654321", "Maria", nil, 2).
			AddRow(otherUUID, wsUUID, nil, "João", nil, 1))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones" WHERE workspace_id = \$1 AND lead_id = ANY\(\$2::uuid\[\]\)`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "number", "label"}).AddRow(phoneUUID, otherUUID, "551133334444", "landline"))

	got, err := NewNumberDirectory(db).FindByNumbers(wsUUID, []string{"5511987654321", "551133334444", "100"})
	if err != nil {
		t.Fatalf("FindByNumbers: %v", err)
	}
	if len(got) != 2 || !got[1].HoldsNumber("551133334444") {
		t.Fatalf("every lead holding a number is returned, with its phones: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"leads.id IN (SELECT id FROM leads WHERE workspace_id = ? AND number = ANY(?) AND deleted_at IS NULL UNION ALL SELECT lead_id FROM lead_phones", "ORDER BY (leads.number = ANY(?)) IS TRUE DESC, leads.id LIMIT ?"} {
		if !strings.Contains(holdingNumbersSQL, want) {
			t.Fatalf("holdingNumbersSQL lacks %q", want)
		}
	}
	if strings.Contains(holdingNumbersSQL, " OR ") {
		t.Fatal("an OR with a sublink scans every lead of the workspace")
	}
}

func TestFindByNumbersOrAddressesCapsEachLookupAndReadsTheContactDetails(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	formats := pq.StringArray{"551133334444"}

	mock.ExpectQuery(exact(holdingNumbersSQL)).
		WithArgs(wsUUID, wsUUID, formats, wsUUID, formats, formats, duplicateLookupLimit).
		WillReturnRows(sqlmock.NewRows(leadColumns).AddRow(leadUUID, wsUUID, nil, "Maria", nil, 2))
	mock.ExpectQuery(exact(livingAtSQL)).
		WithArgs(wsUUID, wsUUID, pq.StringArray{"f-1"}, duplicateLookupLimit).
		WillReturnRows(sqlmock.NewRows(leadColumns).AddRow(leadUUID, wsUUID, nil, "Maria", nil, 2).AddRow(otherUUID, wsUUID, nil, "João", nil, 1))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := (&repository{db: db}).FindByNumbersOrAddresses(context.Background(), wsUUID, []string{"551133334444"}, []string{"f-1", "f-1"})
	if err != nil {
		t.Fatalf("FindByNumbersOrAddresses: %v", err)
	}
	if len(got) != 2 || got[0].ID != leadUUID || got[1].ID != otherUUID || !got[0].IsAggregate() {
		t.Fatalf("found = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestFindOrCreatePromotesTheOnlyLeadWithoutIdentityThatHoldsTheNumber(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads" WHERE workspace_id = \$1 AND number IN`).WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectQuery(`number IS NULL AND \(id IN \(SELECT lead_id FROM lead_phones`).
		WillReturnRows(sqlmock.NewRows(leadColumns).AddRow(leadUUID, wsUUID, nil, "Maria", "manual", 3))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "number", "label"}).AddRow(phoneUUID, leadUUID, "551187654321", "mobile"))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(withDollars(promoteIdentitySQL))).
		WithArgs("5511987654321", sqlmock.AnyArg(), leadUUID, wsUUID, int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(4))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM lead_phones WHERE workspace_id = $1 AND lead_id = $2 AND number = ANY($3)`)).
		WithArgs(wsUUID, leadUUID, `{"5511987654321","551187654321"}`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, created, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, Name: "Mari"})
	if err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}
	if created || got.ID != leadUUID || got.Number != "5511987654321" || len(got.Phones) != 0 || got.Version != 4 {
		t.Fatalf("the contact phone becomes the identity instead of a new lead, got %+v (created %v)", got, created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreateFallsBackToANewLeadWhenThePromotionLosesARace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "leads" WHERE workspace_id = \$1 AND number IN`).WillReturnRows(sqlmock.NewRows(leadColumns))
	mock.ExpectQuery(`number IS NULL AND \(id IN \(SELECT lead_id FROM lead_phones`).
		WillReturnRows(sqlmock.NewRows(leadColumns).AddRow(leadUUID, wsUUID, nil, "Maria", "manual", 3))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "number", "label"}).AddRow(phoneUUID, leadUUID, "5511987654321", "mobile"))
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectRollback()
	mock.ExpectExec(regexp.QuoteMeta(`ON CONFLICT ("workspace_id","number") WHERE deleted_at IS NULL DO NOTHING`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	_, created, err := (&repository{db: db}).FindOrCreate(wsUUID, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel})
	if err != nil || !created {
		t.Fatalf("a lost promotion creates the lead as before, got created %v, %v", created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryNewStatementBindsAsManyArgumentsAsItHasPlaceholders(t *testing.T) {
	statements := map[string]int{
		"lock":     strings.Count(lockLeadsSQL, "?"),
		"shift":    strings.Count(shiftRelationCountsSQL, "?"),
		"update":   strings.Count(updateAddressSQL, "?"),
		"requeue":  strings.Count(requeueAddressSQL, "?"),
		"promote":  strings.Count(promoteIdentitySQL, "?"),
		"save":     strings.Count(saveSQL, "?"),
		"withCols": strings.Count(addressColumnsSQL, "?"),
		"holders":  strings.Count(holdingNumbersSQL, "?"),
		"livingAt": strings.Count(livingAtSQL, "?"),
		"find":     strings.Count(findRelationSQL, "?"),
		"delete":   strings.Count(deleteRelationSQL, "?"),
		"entry":    strings.Count(leadOfEntrySQL, "?"),
		"version":  strings.Count(currentVersionSQL, "?"),
	}
	want := map[string]int{"lock": 2, "shift": 5, "update": 25, "requeue": 26, "promote": 5, "save": 24, "withCols": 22, "holders": 7, "livingAt": 4, "find": 2, "delete": 2, "entry": 3, "version": 2}
	for name, got := range statements {
		if got != want[name] {
			t.Errorf("%s has %d placeholders, want %d", name, got, want[name])
		}
	}
}

func lockLeadsSQLFor() string {
	return withDollars(lockLeadsSQL)
}

func withDollars(sql string) string {
	var b strings.Builder
	n := 0
	for _, r := range sql {
		if r == '?' {
			n++
			b.WriteString("$")
			b.WriteString(itoa(int64(n)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestHasEntriesAsksOnlyWhetherOneExists(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM .* WHERE lead_entries.lead_id = \$1 AND EXISTS \(SELECT 1 FROM leads WHERE leads.id = \$2 AND leads.workspace_id = \$3\)\)`).
		WithArgs(leadUUID, leadUUID, wsUUID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	used, err := (&repository{db: db}).HasEntries(context.Background(), wsUUID, leadUUID)
	if err != nil || !used {
		t.Fatalf("HasEntries = %v, %v", used, err)
	}
	if _, err := (&repository{db: db}).HasEntries(context.Background(), "", leadUUID); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("without a workspace = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func eventValues(leadID string) []driver.Value {
	return []driver.Value{sqlmock.AnyArg(), wsUUID, leadID, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()}
}

func paulistaPostal() address.Postal {
	return paulistaPostalAt("01310100")
}

func paulistaPostalAt(zip string) address.Postal {
	return address.Postal{ZipCode: zip, Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP"}.Normalize()
}

func TestLeadOfEntryReadsTheSharedEntriesSourceInsideTheWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(leadOfEntrySQL)).WithArgs(relationUUID, "unofficial_whatsapp", wsUUID).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id"}).AddRow(leadUUID))
	mock.ExpectQuery(exact(leadOfEntrySQL)).WithArgs(relationUUID, "whatsapp", wsUUID).WillReturnRows(sqlmock.NewRows([]string{"lead_id"}))

	repo := &repository{db: db}
	got, err := repo.LeadOfEntry(context.Background(), wsUUID, shared.EntryRef{EntryID: relationUUID, EntryType: shared.EntryTypeUnofficialWhatsApp})
	if err != nil || got != leadUUID {
		t.Fatalf("LeadOfEntry = %q, %v", got, err)
	}
	if _, err := repo.LeadOfEntry(context.Background(), wsUUID, shared.EntryRef{EntryID: relationUUID, EntryType: shared.EntryTypeWhatsApp}); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("an entry without a lead = %v", err)
	}
	if _, err := repo.LeadOfEntry(context.Background(), wsUUID, shared.EntryRef{EntryID: "x", EntryType: shared.EntryTypeWhatsApp}); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("a malformed entry = %v, want not found without a query", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
