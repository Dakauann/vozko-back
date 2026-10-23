package carriermessaging_repository

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
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

var testNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

var messageColumns = []string{
	"id", "workspace_id", "reference", "sender_id", "phone", "lead_id", "conversation_id",
	"run_kind", "marketing", "run_id", "variant", "content", "class", "step", "status", "error_code",
	"parent_message_id", "fallback_message_id", "fallback", "charge_reference",
	"accepted_at", "delivered_at", "read_at", "failed_at", "expires_at", "created_at", "updated_at",
	"channel", "text", "segments", "operator",
}

type messageRow struct {
	channel  string
	status   string
	step     string
	runKind  string
	class    string
	content  string
	fallback string
}

func defaultMessageRow() messageRow {
	return messageRow{channel: "rcs", status: "accepted", step: "rcs", runKind: "campaign", class: "basic", content: `{"text":"oi"}`, fallback: `{"sms_text":"oi"}`}
}

func (m messageRow) values() []driver.Value {
	return []driver.Value{
		"m1", "ws1", "ref1", "s1", "5511999990000", nil, nil,
		m.runKind, true, "run1", 0, []byte(m.content), m.class, m.step, m.status, "",
		nil, nil, []byte(m.fallback), "",
		testNow, nil, nil, nil, testNow.Add(time.Hour), testNow, testNow,
		m.channel, "Loja: oi", 1, "vivo",
	}
}

func messageRows(rows ...messageRow) *sqlmock.Rows {
	out := sqlmock.NewRows(messageColumns)
	for _, r := range rows {
		out.AddRow(r.values()...)
	}
	return out
}
