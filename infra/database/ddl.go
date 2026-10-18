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
		if err := db.Exec(s.sql).Error; err != nil {
			return fmt.Errorf("creating %s: %w", s.name, err)
		}
	}
	return nil
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
