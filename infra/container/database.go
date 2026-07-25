package container

import (
	"log"

	"vozko/infra/database"
)

func (c *Container) initDatabase() {
	// Run schema migrations and index DDL on a dedicated simple-protocol
	// connection. Sharing the application's PrepareStmt pool here causes
	// "cached plan must not change result type" (0A000) once AutoMigrate alters
	// a table it already introspected, which aborts the transaction and crashes
	// the postgres driver. See database.NewMigrationDatabase.
	migrationDB, err := database.NewMigrationDatabase()
	if err != nil {
		log.Fatal("Failed to connect to database for migrations:", err)
	}

	if err := database.RunMigrations(migrationDB); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	database.CreatePerformanceIndexes(migrationDB)

	if sqlDB, err := migrationDB.DB(); err == nil {
		_ = sqlDB.Close()
	}

	db, err := database.NewGormDatabase()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	c.db = db
}
