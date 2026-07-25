package shortlink

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
		WithoutReturning:     true,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	return db, mock, sqlDB
}

var shortLinkColumns = []string{
	"id", "workspace_id", "department_id", "created_by", "code", "target_url",
	"title", "redirect_type", "status", "password_hash", "expires_at", "max_clicks",
	"click_count", "unique_click_count", "last_clicked_at", "created_at", "updated_at", "deleted_at",
}

func clickColumns() []string {
	return []string{
		"id", "short_link_id", "workspace_id", "occurred_at", "ip_hash", "country",
		"region", "city", "device_type", "os", "browser", "referer_domain",
		"utm_source", "utm_medium", "utm_campaign", "is_bot", "is_proxy", "language", "created_at",
	}
}
