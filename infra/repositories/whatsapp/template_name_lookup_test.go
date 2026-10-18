package whatsapp_repository

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const lookupWorkspace = "11111111-1111-1111-1111-111111111111"

func mockedNameLookup(t *testing.T) (*grantedTemplateNames, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	return &grantedTemplateNames{db: db}, mock
}

func numberedPlaceholders(sql string) string {
	quoted := regexp.QuoteMeta(sql)
	var out strings.Builder
	position := 0
	for {
		before, after, found := strings.Cut(quoted, `\?`)
		out.WriteString(before)
		if !found {
			break
		}
		position++
		out.WriteString(`\$` + strconv.Itoa(position))
		quoted = after
	}
	return "^" + out.String() + "$"
}

func TestGrantedTemplatesByNameBindsWorkspaceNameAndLanguage(t *testing.T) {
	if got := strings.Count(grantedTemplatesByNameSQL, "?"); got != 3 {
		t.Fatalf("grantedTemplatesByNameSQL has %d placeholders, the call binds 3", got)
	}
	for _, want := range []string{"JOIN workspace_template_access", "grants.workspace_id = ?", "lower(t.name) = ?", "t.language = ?", "t.deleted_at IS NULL"} {
		if !strings.Contains(grantedTemplatesByNameSQL, want) {
			t.Fatalf("grantedTemplatesByNameSQL lacks %q", want)
		}
	}
	repo, mock := mockedNameLookup(t)
	now := time.Now()
	mock.ExpectQuery(numberedPlaceholders(grantedTemplatesByNameSQL)).
		WithArgs(lookupWorkspace, "promo_outubro", "pt_BR").
		WillReturnRows(sqlmock.NewRows([]string{"id", "external_id", "waba_id", "name", "language", "category", "status", "parameter_format", "components", "created_at", "updated_at"}).
			AddRow("t1", "e1", "waba-1", "promo_outubro", "pt_BR", "MARKETING", "APPROVED", "positional", []byte(`[{"type":"BUTTONS","buttons":[{"type":"QUICK_REPLY","text":"SAIR"}]}]`), now, now).
			AddRow("t2", "e2", "waba-2", "Promo_Outubro", "pt_BR", "MARKETING", "APPROVED", "positional", []byte(`[]`), now, now))

	got, err := repo.FindGrantedByName(lookupWorkspace, "promo_outubro", "pt_BR")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "t1" || got[1].WABAId != "waba-2" || len(got[0].QuickReplies()) != 1 {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGrantedTemplatesByNameRefusesWithoutAWorkspace(t *testing.T) {
	repo, mock := mockedNameLookup(t)
	if _, err := repo.FindGrantedByName(" ", "promo_outubro", "pt_BR"); err == nil {
		t.Fatal("a missing workspace must refuse")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
