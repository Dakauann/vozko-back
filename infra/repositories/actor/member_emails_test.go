package actor_repository

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserIDsByEmailBindsOneArrayAndAnswersByLowercaseEmail(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(membersByEmailSQL, "?"); got != 2 {
		t.Fatalf("placeholders = %d, want 2", got)
	}
	mock.ExpectQuery("^"+strings.ReplaceAll(regexp.QuoteMeta(membersByEmailSQL), `\?`, `\$[0-9]+`)+"$").
		WithArgs("ws-1", pq.StringArray{"clara@escola.com", "rui@escola.com"}).
		WillReturnRows(sqlmock.NewRows([]string{"email", "id"}).AddRow("clara@escola.com", "u-clara"))

	got, err := NewMemberEmails(db).UserIDsByEmail(context.Background(), "ws-1", []string{" Clara@Escola.com ", "rui@escola.com", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["clara@escola.com"] != "u-clara" {
		t.Fatalf("got = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if empty, err := NewMemberEmails(db).UserIDsByEmail(context.Background(), "ws-1", nil); err != nil || len(empty) != 0 {
		t.Fatalf("no e-mails = %+v, %v", empty, err)
	}
}
