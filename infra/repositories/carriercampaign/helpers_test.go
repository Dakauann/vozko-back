package carriercampaign_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	campaignID = "6f1c2a9e-1d55-4f6e-9a0c-3b8f1f7d2a10"
	runID      = "0b7f3c1e-8a2d-4c5b-9e6f-1a2b3c4d5e6f"
)

var testNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func expectationsMet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var campaignColumns = []string{"id", "workspace_id", "name", "sender_id", "channel", "status", "kind", "variants", "fallback", "audience", "scheduled_at", "recurrence", "created_by", "created_at", "updated_at"}

type campaignRow struct {
	status, kind, channel, variants, audience string
	recurrence                                any
}

func defaultCampaignRow() campaignRow {
	return campaignRow{
		status: "scheduled", kind: "marketing", channel: "rcs",
		variants:   `[{"name":"A","content":{"text":"oi"}}]`,
		audience:   `{"mode":"first_n","limit":50,"expected_count":40,"fingerprint":"fp"}`,
		recurrence: []byte(`{"frequency":"daily","time":"09:00","timezone":"America/Sao_Paulo"}`),
	}
}

func campaignRows(rows ...campaignRow) *sqlmock.Rows {
	out := sqlmock.NewRows(campaignColumns)
	for _, r := range rows {
		out.AddRow(campaignID, "ws1", "Black Friday", "s1", r.channel, r.status, r.kind, []byte(r.variants),
			[]byte(`{"sms":{"route_id":"r1","text":"oi"}}`), []byte(r.audience), testNow, r.recurrence, "u1", testNow, testNow)
	}
	return out
}
