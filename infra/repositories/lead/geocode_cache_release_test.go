package lead

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/lead"
)

func TestStoredAnswerReleaseStatementsBindEveryPlaceholder(t *testing.T) {
	for sql, want := range map[string]int{lockStoredAnswersSQL: 2, releaseStoredAnswersSQL: 2, heldFingerprintsSQL: 2} {
		if got := countPlaceholders(sql); got != want {
			t.Errorf("%q has %d placeholders, want %d", sql, got, want)
		}
	}
	for sql, fragments := range map[string][]string{
		lockStoredAnswersSQL:    {"c.workspace_id = ?", "c.fingerprint = ANY(?::text[])", "ORDER BY c.fingerprint FOR UPDATE"},
		releaseStoredAnswersSQL: {"DELETE FROM geocode_cache c WHERE c.workspace_id = ?", "c.fingerprint = ANY(?::text[])", "holder.deleted_at IS NULL", "o.workspace_id = c.workspace_id AND o.fingerprint = c.fingerprint"},
	} {
		for _, fragment := range fragments {
			if !strings.Contains(sql, fragment) {
				t.Errorf("%q misses %q", sql, fragment)
			}
		}
	}
}

func expectRelease(mock sqlmock.Sqlmock, fingerprints pq.StringArray, stored ...string) {
	rows := sqlmock.NewRows([]string{"fingerprint"})
	for _, f := range stored {
		rows.AddRow(f)
	}
	mock.ExpectQuery(exact(lockStoredAnswersSQL)).WithArgs(wsUUID, fingerprints).WillReturnRows(rows)
	if len(stored) > 0 {
		mock.ExpectExec(exact(releaseStoredAnswersSQL)).WithArgs(wsUUID, pq.StringArray(stored)).WillReturnResult(sqlmock.NewResult(0, int64(len(stored))))
	}
}

func TestSaveReleasesTheStoredAnswerOfATextTheLeadNoLongerHolds(t *testing.T) {
	tests := []struct {
		name   string
		stored []string
	}{
		{"a stored answer is released", []string{"f-old"}},
		{"nothing is deleted when no answer is stored", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
			mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(storedAddressRow("01310100"))
			mock.ExpectExec(exact(requeueAddressSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
			expectRelease(mock, pq.StringArray{"f-old"}, tt.stored...)
			mock.ExpectCommit()

			l := savedLead()
			l.Addresses = []lead.Address{{ID: addressUUID, Label: lead.AddressHome, Primary: true, GeoStatus: lead.GeoApproximate, Postal: paulistaPostal()}}
			if err := l.SetAddresses([]lead.AddressInput{{ID: addressUUID, Label: lead.AddressHome, Primary: true, Postal: paulistaPostalAt("01311000")}}); err != nil {
				t.Fatal(err)
			}
			if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSaveReleasesTheStoredAnswerOfARemovedAddress(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(storedAddressRow("01310100"))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM lead_addresses WHERE workspace_id = $1 AND lead_id = $2 AND id = ANY($3::uuid[])`)).
		WithArgs(wsUUID, leadUUID, pq.StringArray{addressUUID}).WillReturnResult(sqlmock.NewResult(0, 1))
	expectRelease(mock, pq.StringArray{"f-old"}, "f-old")
	mock.ExpectCommit()

	l := savedLead()
	if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveKeepsTheStoredAnswerOfATextTheLeadStillHolds(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	l := savedLead()
	l.Addresses = []lead.Address{{ID: addressUUID, Label: lead.AddressWork, Primary: true, GeoStatus: lead.GeoApproximate, Postal: paulistaPostal()}}
	held := addressRow(l, 0, l.Addresses[0], l.UpdatedAt).Fingerprint

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
	mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "label", "is_primary", "position", "zip_code", "street", "number", "district", "city", "state", "geo_status", "fingerprint"}).
			AddRow(addressUUID, wsUUID, leadUUID, "home", true, 0, "01310100", "Avenida Paulista", "1000", "Bela Vista", "São Paulo", "SP", "approximate", held).
			AddRow(phoneUUID, wsUUID, leadUUID, "work", false, 1, "01310100", "Avenida Paulista", "1000", "Bela Vista", "São Paulo", "SP", "approximate", held))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM lead_addresses`)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(exact(updateAddressSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
