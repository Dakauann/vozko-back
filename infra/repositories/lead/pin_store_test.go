package lead

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/geo"
	"vozko/domain/lead"
)

func storedPinnedRow(source string, lat, lng float64) *sqlmock.Rows {
	unchanged := addressRow(savedLead(), 0, lead.Address{ID: addressUUID, Label: lead.AddressHome, Primary: true, Postal: paulistaPostal()}, time.Now())
	return sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "label", "is_primary", "position", "zip_code", "street", "number", "district", "district_key",
		"city", "city_key", "state", "latitude", "longitude", "geo_precision", "geo_source", "geo_status", "fingerprint"}).
		AddRow(addressUUID, wsUUID, leadUUID, "home", true, 0, string(unchanged.ZipCode), string(unchanged.Street), string(unchanged.Number), string(unchanged.District),
			string(unchanged.DistrictKey), string(unchanged.City), string(unchanged.CityKey), string(unchanged.State), lat, lng, "postal_code", source, "approximate", unchanged.Fingerprint)
}

func TestSaveReleasesTheGeocodeClaimWhenAPositionChanges(t *testing.T) {
	pin := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: time.Now()}
	imported := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionAddress, Source: geo.SourceImport, FixedAt: time.Now()}
	cases := []struct {
		name  string
		fix   geo.Fix
		label lead.AddressLabel
		sql   string
	}{
		{"a pin over a reference point drops the sweeper's claim", pin, lead.AddressHome, repositionAddressSQL},
		{"an imported position under the same text drops the sweeper's claim", imported, lead.AddressHome, repositionAddressSQL},
		{"a label change under the same position keeps the claim", geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionPostalCode, Source: geo.SourceReference}, lead.AddressWork, updateAddressSQL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(`UPDATE leads SET number`).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(5))
			mock.ExpectQuery(`SELECT \* FROM "lead_phones"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(`SELECT \* FROM "lead_addresses"`).WillReturnRows(storedPinnedRow("reference", -23.56, -46.65))
			args := append(anyArgs(22), addressUUID, leadUUID, wsUUID)
			args[0], args[13], args[14], args[15], args[16] = string(tc.label), tc.fix.Point.Lat, tc.fix.Point.Lng, string(tc.fix.Precision), string(tc.fix.Source)
			mock.ExpectExec(exact(tc.sql)).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			l := savedLead()
			fix := tc.fix
			l.Addresses = []lead.Address{{ID: addressUUID, Label: tc.label, Primary: true, Postal: paulistaPostal(), Fix: &fix, GeoStatus: lead.StatusOfFix(fix)}}
			if err := (&repository{db: db}).Save(context.Background(), l, 4, nil); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTheRepositionStatementBindsAsManyArgumentsAsTheUpdate(t *testing.T) {
	if got, want := strings.Count(repositionAddressSQL, "?"), strings.Count(updateAddressSQL, "?"); got != want {
		t.Fatalf("repositionAddressSQL has %d placeholders, want %d", got, want)
	}
	if !strings.Contains(repositionAddressSQL, "geo_claim = NULL") {
		t.Fatalf("repositionAddressSQL = %s", repositionAddressSQL)
	}
}
