package facebook_repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
)

func withPII(t *testing.T) {
	t.Helper()
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	piigorm.SetService(s)
	t.Cleanup(func() { piigorm.SetService(nil) })
}

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	withPII(t)
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func TestCreatingAPageLinkedElsewhereIsReported(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "facebook_pages"`)).
		WillReturnError(errors.New(`pq: duplicate key value violates unique constraint "idx_facebook_pages_fb_page_id"`))
	err := NewPageRepository(db).Create(context.Background(), &fbdomain.Page{WorkspaceID: "ws", FBPageID: "1", Status: fbdomain.StatusConnected})
	if !errors.Is(err, fbdomain.ErrPageAlreadyLinked) {
		t.Fatalf("got %v", err)
	}
}

func TestGrantUpsertCreatesWhenAbsent(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "facebook_grants" WHERE (workspace_id = $1 AND app_scoped_user_id = $2 AND client_business_id = $3)`)).
		WithArgs("ws", "asid", "biz", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "facebook_grants"`)).WillReturnResult(sqlmock.NewResult(0, 1))

	g := &fbdomain.Grant{WorkspaceID: "ws", AppScopedUserID: "asid", ClientBusinessID: "biz", GranularScopes: map[string][]string{"pages_show_list": {"1"}}}
	if err := NewGrantRepository(db).Upsert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if g.ID == "" {
		t.Fatal("new grant id not propagated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGrantUpsertUpdatesExistingAndReactivates(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "facebook_grants"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id"}).AddRow("g-1", "ws"))
	mock.ExpectExec(`UPDATE "facebook_grants" SET .*"revoked_at"=\$5,"scopes"=\$6,"status"=\$7`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), nil, sqlmock.AnyArg(), "ACTIVE",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "g-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	g := &fbdomain.Grant{WorkspaceID: "ws", AppScopedUserID: "asid"}
	if err := NewGrantRepository(db).Upsert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if g.ID != "g-1" {
		t.Fatalf("id = %q", g.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestContactFindOrCreateIgnoresConcurrentInsert(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "facebook_contacts"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "facebook_contacts" .* ON CONFLICT \("page_id","psid"\) WHERE deleted_at IS NULL DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "facebook_contacts"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "page_id", "psid"}).AddRow("c-1", "p-1", "psid-1"))

	c, err := NewContactRepository(db).FindOrCreate(context.Background(), "ws", "p-1", "psid-1")
	if err != nil || c.ID != "c-1" {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestWatermarkUpdateIsMonotonic(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_conversations" SET "read_watermark"=GREATEST\(COALESCE\(read_watermark, \$1\), \$2\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewConversationRepository(db).AdvanceWatermark(context.Background(), "c-1", fbdomain.WatermarkRead, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestMergeMetadataAppendsJSON(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_conversations" SET "metadata"=COALESCE\(metadata, '\{\}'::jsonb\) \|\| \$1::jsonb`).
		WithArgs(`{"referral_ref":"promo"}`, sqlmock.AnyArg(), "c-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewConversationRepository(db).MergeMetadata(context.Background(), "c-1", map[string]any{"referral_ref": "promo"}); err != nil {
		t.Fatal(err)
	}
}

func TestContactProfileDeniedKeepsTheExistingName(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_contacts" SET "profile_fetched_at"=\$1,"profile_status"=\$2,"updated_at"=\$3 WHERE`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := NewContactRepository(db).UpdateProfile(context.Background(), "c-1", fbdomain.ContactProfile{Status: fbdomain.ProfileDenied, FetchedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSeedMetadataLetsExistingKeysWin(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "facebook_conversations" SET "metadata"=\$1::jsonb \|\| COALESCE\(metadata, '\{\}'::jsonb\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewConversationRepository(db).SeedMetadata(context.Background(), "c-1", map[string]any{"facebook_first_referral_ref": "promo"}); err != nil {
		t.Fatal(err)
	}
}

func TestLatestForPageOrdersByTheCustomersLastMessage(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT \* FROM "facebook_conversations" WHERE page_id = \$1 AND "facebook_conversations"."deleted_at" IS NULL ORDER BY last_customer_message_at DESC NULLS LAST`).
		WithArgs("p-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "page_id"}).AddRow("c-9", "p-1"))
	conv, err := NewConversationRepository(db).LatestForPage(context.Background(), "p-1")
	if err != nil || conv.ID != "c-9" {
		t.Fatalf("got %+v, %v", conv, err)
	}
}
