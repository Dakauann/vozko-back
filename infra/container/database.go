package container

import (
	"log"

	"vozko/infra/database"
)

func (c *Container) initDatabase() {
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
