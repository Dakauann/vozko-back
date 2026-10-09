package lead

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/lead"
)

const (
	relativeUUID = "3a9b7c5d-1e2f-4a6b-8c0d-2e4f6a8b0c1d"
	relationUUID = "5c7d9e1f-3a5b-4c7d-9e1f-3a5b7c9d1e2f"
)

var anonymizedAt = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

func exact(sql string) string {
	quoted := regexp.QuoteMeta(sql)
	var numbered strings.Builder
	position := 0
	for {
		before, after, found := strings.Cut(quoted, `\?`)
		numbered.WriteString(before)
		if !found {
			break
		}
		position++
		numbered.WriteString(`\$` + strconv.Itoa(position))
		quoted = after
	}
	return "^" + numbered.String() + "$"
}

var (
	identityForms = pq.StringArray{"5511987654321", "551187654321", "+5511987654321", "+551187654321"}
	landlineForms = pq.StringArray{"551133334444", "+551133334444"}
)

func masked(n int, mask string) pq.StringArray {
	out := make(pq.StringArray, n)
	for i := range out {
		out[i] = mask
	}
	return out
}

func expectSubjectLocked(mock sqlmock.Sqlmock, counterparts *sqlmock.Rows, locked pq.StringArray) {
	mock.ExpectBegin()
	mock.ExpectQuery(exact(relationCounterpartsSQL)).WithArgs(leadUUID, wsUUID, leadUUID, leadUUID).WillReturnRows(counterparts)
	mock.ExpectQuery(exact(lockLeadsSQL)).WithArgs(wsUUID, locked).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(leadUUID))
}

func subjectRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "workspace_id", "number", "name", "email", "version"}).
		AddRow(leadUUID, wsUUID, "5511987654321", "Maria", "maria@exemplo.com.br", 8)
}

func expectAnonymization(mock sqlmock.Sqlmock) {
	expectSubjectLocked(mock, sqlmock.NewRows([]string{"id"}).AddRow(relativeUUID), pq.StringArray{relativeUUID, leadUUID})
	mock.ExpectQuery(exact(anonymizedLeadSQL)).WithArgs(leadUUID, wsUUID).WillReturnRows(subjectRow())
	mock.ExpectQuery(exact(contactNumbersSQL)).WithArgs(wsUUID, leadUUID).
		WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow("551133334444"))
	mock.ExpectQuery(exact(unofficialEntryNumbersSQL)).WithArgs(wsUUID, leadUUID).
		WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow("5511987654321"))

	mock.ExpectQuery(exact(unrelateAllSQL)).WithArgs(wsUUID, leadUUID, leadUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "other_lead_id", "dimension", "kind", "created_at"}).
			AddRow(relationUUID, wsUUID, leadUUID, relativeUUID, "family", "child", anonymizedAt))
	mock.ExpectQuery(exact(shiftRelationCountsSQL)).
		WithArgs(anonymizedAt, pq.StringArray{relativeUUID}, pq.Int64Array{-1}, pq.Int64Array{0}, wsUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "relatives_count", "referred_count"}).AddRow(relativeUUID, 4, 0, 0))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(exact(heldByOthersSQL)).WithArgs(landlineForms, wsUUID, leadUUID, wsUUID, leadUUID).
		WillReturnRows(sqlmock.NewRows([]string{"form"}).AddRow("551133334444"))

	mock.ExpectQuery(exact(heldFingerprintsSQL)).WithArgs(wsUUID, leadUUID).WillReturnRows(sqlmock.NewRows([]string{"fingerprint"}).AddRow("f-home"))
	mock.ExpectExec(exact(erasePhonesSQL)).WithArgs(wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(exact(eraseAddressesSQL)).WithArgs(wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(exact(eraseMemoriesSQL)).WithArgs(wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(exact(redactLeadEventsSQL)).WithArgs(wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectExec(exact(eraseCallListItemsSQL)).WithArgs(wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 2))
	expectRelease(mock, pq.StringArray{"f-home"}, "f-home")
	mock.ExpectExec(exact(eraseCampaignEntriesSQL)).WithArgs(leadUUID).WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec(exact(eraseUnofficialEntriesSQL)).
		WithArgs(pq.StringArray{"5511987654321"}, pq.StringArray{"••••4321"}, wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 6))

	own := append(append(pq.StringArray{}, identityForms...), landlineForms...)
	ownMasks := append(masked(4, "••••4321"), masked(2, "••••4444")...)
	unownedMasks := masked(4, "••••4321")
	mock.ExpectExec(exact(maskOwnCallsSQL)).
		WithArgs(own, ownMasks, own, ownMasks, wsUUID, own, own, leadUUID).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(exact(maskUnownedCallsSQL)).
		WithArgs(identityForms, unownedMasks, identityForms, unownedMasks, wsUUID, identityForms, identityForms).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(exact(maskOwnSendsSQL)).
		WithArgs(own, ownMasks, wsUUID, own, leadUUID).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(exact(maskUnownedSendsSQL)).
		WithArgs(identityForms, unownedMasks, wsUUID, identityForms).WillReturnResult(sqlmock.NewResult(0, 4))

	mock.ExpectQuery(exact(anonymizeLeadSQL)).
		WithArgs(
			nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, false, nil, nil, "", nil,
			anonymizedAt, anonymizedAt, leadUUID, wsUUID,
		).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(9))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func TestAnonymizeTouchesEveryErasureTargetInOneTransaction(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAnonymization(mock)

	erasure, err := (&repository{db: db}).Anonymize(context.Background(), wsUUID, leadUUID, actorUUID, anonymizedAt)
	if err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	want := map[lead.ErasureTarget]int64{
		lead.ErasureRecord: 1, lead.ErasurePhones: 1, lead.ErasureAddresses: 2, lead.ErasureRelations: 1, lead.ErasureEvents: 4,
		lead.ErasureMemories: 3, lead.ErasureCampaignEntries: 5, lead.ErasureUnofficialCampaignEntries: 6,
		lead.ErasureCallNumbers: 3, lead.ErasureTemplateSendNumbers: 7, lead.ErasureCallListItems: 2, lead.ErasureGeocodeCache: 1,
	}
	for _, target := range lead.ErasureTargets() {
		if got, touched := erasure.Rows[target]; !touched || got != want[target] {
			t.Errorf("%s: rows = %d (touched %v), want %d", target, got, touched, want[target])
		}
	}
	if erasure.LeadID != leadUUID || erasure.Version != 9 || !erasure.At.Equal(anonymizedAt) || len(erasure.Counterparts) != 1 || erasure.Counterparts[relativeUUID].Version != 4 {
		t.Fatalf("erasure = %+v", erasure)
	}
}

func TestAnonymizeAnswersNotFoundForAGoneLeadAndWritesNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectSubjectLocked(mock, sqlmock.NewRows([]string{"id"}), pq.StringArray{leadUUID})
	mock.ExpectQuery(exact(anonymizedLeadSQL)).WithArgs(leadUUID, wsUUID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	_, err := (&repository{db: db}).Anonymize(context.Background(), wsUUID, leadUUID, actorUUID, anonymizedAt)
	if !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("err = %v, want ErrLeadNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnonymizeWithoutNumbersMasksNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectSubjectLocked(mock, sqlmock.NewRows([]string{"id"}), pq.StringArray{leadUUID})
	mock.ExpectQuery(exact(anonymizedLeadSQL)).WithArgs(leadUUID, wsUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "version"}).AddRow(leadUUID, wsUUID, "Ana", 2))
	mock.ExpectQuery(exact(contactNumbersSQL)).WillReturnRows(sqlmock.NewRows([]string{"number"}))
	mock.ExpectQuery(exact(unofficialEntryNumbersSQL)).WillReturnRows(sqlmock.NewRows([]string{"number"}))
	mock.ExpectQuery(exact(unrelateAllSQL)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(exact(heldFingerprintsSQL)).WillReturnRows(sqlmock.NewRows([]string{"fingerprint"}))
	for _, sql := range []string{erasePhonesSQL, eraseAddressesSQL, eraseMemoriesSQL, redactLeadEventsSQL, eraseCallListItemsSQL, eraseCampaignEntriesSQL} {
		mock.ExpectExec(exact(sql)).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec(exact(eraseUnofficialEntriesSQL)).WithArgs(pq.StringArray{}, pq.StringArray{}, wsUUID, leadUUID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(exact(anonymizeLeadSQL)).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	erasure, err := (&repository{db: db}).Anonymize(context.Background(), wsUUID, leadUUID, actorUUID, anonymizedAt)
	if err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if erasure.Rows[lead.ErasureCallNumbers] != 0 || erasure.Rows[lead.ErasureTemplateSendNumbers] != 0 {
		t.Fatalf("erasure = %+v", erasure.Rows)
	}
}

func TestAnonymizeRollsBackWhenAnyStepFails(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectSubjectLocked(mock, sqlmock.NewRows([]string{"id"}), pq.StringArray{leadUUID})
	mock.ExpectQuery(exact(anonymizedLeadSQL)).WillReturnRows(subjectRow())
	mock.ExpectQuery(exact(contactNumbersSQL)).WillReturnRows(sqlmock.NewRows([]string{"number"}))
	mock.ExpectQuery(exact(unofficialEntryNumbersSQL)).WillReturnRows(sqlmock.NewRows([]string{"number"}))
	mock.ExpectQuery(exact(unrelateAllSQL)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(exact(heldFingerprintsSQL)).WillReturnRows(sqlmock.NewRows([]string{"fingerprint"}))
	mock.ExpectExec(exact(erasePhonesSQL)).WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := (&repository{db: db}).Anonymize(context.Background(), wsUUID, leadUUID, actorUUID, anonymizedAt); err == nil {
		t.Fatal("a failed step must fail the whole anonymization")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnonymizeRefusesAMissingWorkspaceOrLead(t *testing.T) {
	r := &repository{}
	if _, err := r.Anonymize(context.Background(), "", leadUUID, actorUUID, anonymizedAt); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.Anonymize(context.Background(), wsUUID, " ", actorUUID, anonymizedAt); !errors.Is(err, lead.ErrLeadRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnonymizeMasksOnlyRowsThatBelongToThePerson(t *testing.T) {
	for name, sql := range map[string]string{"own calls": maskOwnCallsSQL, "own sends": maskOwnSendsSQL} {
		if !strings.Contains(sql, "lead_id = ?") {
			t.Errorf("%s must be scoped to the person: %s", name, sql)
		}
	}
	for name, sql := range map[string]string{"unowned calls": maskUnownedCallsSQL, "unowned sends": maskUnownedSendsSQL} {
		if !strings.Contains(sql, "IS NULL") {
			t.Errorf("%s must only touch rows that belong to nobody: %s", name, sql)
		}
	}
	if !strings.Contains(anonymizeLeadSQL, recordColumnsSQL) || !strings.Contains(saveSQL, recordColumnsSQL) {
		t.Error("the anonymized record is written through the same columns as a save")
	}
}

func TestAnonymizeStatementsBindEveryPlaceholder(t *testing.T) {
	cases := map[string]int{
		relationCounterpartsSQL: 4, anonymizedLeadSQL: 2, contactNumbersSQL: 2, unofficialEntryNumbersSQL: 2, unrelateAllSQL: 3,
		heldByOthersSQL: 5, erasePhonesSQL: 2, eraseAddressesSQL: 2, eraseMemoriesSQL: 2, redactLeadEventsSQL: 2, eraseCallListItemsSQL: 2,
		eraseCampaignEntriesSQL: 1, eraseUnofficialEntriesSQL: 4, maskOwnCallsSQL: 8, maskUnownedCallsSQL: 7,
		maskOwnSendsSQL: 5, maskUnownedSendsSQL: 4, anonymizeLeadSQL: 23,
	}
	for sql, want := range cases {
		if got := countPlaceholders(sql); got != want {
			t.Errorf("%q has %d placeholders, want %d", sql, got, want)
		}
	}
}

func countPlaceholders(sql string) int {
	count := 0
	for _, r := range sql {
		if r == '?' {
			count++
		}
	}
	return count
}

func TestAnonymizeReadsThePersonsAddressTextsToReleaseTheirStoredAnswers(t *testing.T) {
	if heldFingerprintsSQL != "SELECT DISTINCT fingerprint FROM lead_addresses WHERE workspace_id = ? AND lead_id = ?" {
		t.Fatalf("held fingerprints SQL = %q", heldFingerprintsSQL)
	}
}
