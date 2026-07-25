package database

import (
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func buildDSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
	)
}

// NewGormDatabase opens the connection pool used for application traffic.
// PrepareStmt is enabled here for runtime performance; it must NOT be used for
// schema migrations (see NewMigrationDatabase).
func NewGormDatabase() (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(buildDSN()), &gorm.Config{
		PrepareStmt: true,
		Logger:      logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	return db, nil
}

// NewMigrationDatabase opens a short-lived connection dedicated to running
// schema migrations. It uses the Postgres simple query protocol with no
// prepared-statement cache, because AutoMigrate performs DDL on the same tables
// it introspects in the same session. With prepared statements enabled, a cached
// plan (e.g. "SELECT * FROM leads LIMIT 1") goes stale the moment a column is
// added/altered, producing "cached plan must not change result type" (0A000),
// which aborts the migration transaction (25P02) and ultimately crashes the
// driver's AlterColumn with a nil-pointer panic.
//
// The caller is responsible for closing the returned *gorm.DB's *sql.DB.
func NewMigrationDatabase() (*gorm.DB, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  buildDSN(),
		PreferSimpleProtocol: true, // disable implicit prepared statements
	}), &gorm.Config{
		PrepareStmt: false,
		Logger:      logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	// A single connection keeps the advisory lock and DDL on one session.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	return db, nil
}
