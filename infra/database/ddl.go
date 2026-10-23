package database

import (
	"fmt"

	"gorm.io/gorm"
)

type ddl struct {
	name string
	sql  string
}

func execDDL(db *gorm.DB, statements []ddl) error {
	for _, s := range statements {
		if err := createUnlessPresent(db, s); err != nil {
			return fmt.Errorf("creating %s: %w", s.name, err)
		}
	}
	return nil
}

func createUnlessPresent(db *gorm.DB, s ddl) error {
	var present bool
	err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_index i ON i.indexrelid = c.oid
			WHERE c.relname = ? AND i.indisvalid AND i.indisready
		)
	`, s.name).Scan(&present).Error
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	return db.Exec(s.sql).Error
}

func lookupDDL(name string, statements []ddl) (string, bool) {
	for _, s := range statements {
		if s.name == name {
			return s.sql, true
		}
	}
	return "", false
}

func ddlNames(statements []ddl) []string {
	names := make([]string, len(statements))
	for i, s := range statements {
		names[i] = s.name
	}
	return names
}
