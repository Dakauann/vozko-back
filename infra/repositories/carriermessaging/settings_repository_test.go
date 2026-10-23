package carriermessaging_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriermessaging"
	"vozko/domain/working_hours"
)

var settingsColumns = []string{"workspace_id", "quiet_hours", "frequency_cap_per_day", "frequency_cap_per_week", "attribution_days", "created_at", "updated_at"}

func TestSettingsGet(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_settings" WHERE workspace_id = \$1`).WithArgs("ws1", 1).
		WillReturnRows(sqlmock.NewRows(settingsColumns).
			AddRow("ws1", []byte(`{"timezone":"America/Sao_Paulo","days":{"mon":[{"start":"09:00","end":"18:00"}]}}`), 3, 9, 14, testNow, testNow))
	got, err := NewSettingsRepository(db).Get(context.Background(), "ws1")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != "ws1" || got.FrequencyCap != (carriermessaging.FrequencyCap{PerDay: 3, PerWeek: 9}) || got.AttributionDays != 14 ||
		got.QuietHours.Timezone != "America/Sao_Paulo" || got.QuietHours.Days["mon"][0].End != "18:00" {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestSettingsGetWithoutARowIsNil(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_settings"`).WillReturnRows(sqlmock.NewRows(settingsColumns))
	got, err := NewSettingsRepository(db).Get(context.Background(), "ws1")
	if err != nil || got != nil {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestSettingsGetFailsClosed(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
	}{
		{"query error", func(m sqlmock.Sqlmock) { m.ExpectQuery(`SELECT`).WillReturnError(boom) }},
		{"broken quiet hours", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(settingsColumns).AddRow("ws1", []byte(`{"days":`), 1, 1, 1, testNow, testNow))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			tc.setup(mock)
			if got, err := NewSettingsRepository(db).Get(context.Background(), "ws1"); err == nil || got != nil {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestSettingsPutUpserts(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "carrier_settings" .* ON CONFLICT \("workspace_id"\) DO UPDATE SET "quiet_hours"="excluded"."quiet_hours","frequency_cap_per_day"="excluded"."frequency_cap_per_day","frequency_cap_per_week"="excluded"."frequency_cap_per_week","attribution_days"="excluded"."attribution_days","updated_at"="excluded"."updated_at"`).
		WithArgs("ws1", `{"timezone":"America/Sao_Paulo","days":{}}`, 2, 6, 7, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	s := carriermessaging.Settings{WorkspaceID: "ws1", FrequencyCap: carriermessaging.DefaultFrequencyCap(), AttributionDays: 7}
	s.QuietHours.Timezone = "America/Sao_Paulo"
	s.QuietHours.Days = map[string][]working_hours.Window{}
	if err := NewSettingsRepository(db).Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}
