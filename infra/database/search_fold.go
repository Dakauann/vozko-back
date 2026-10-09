package database

import "gorm.io/gorm"

const SearchFoldFunction = "vozko_fold"

func SearchFold(expr string) string {
	return SearchFoldFunction + "(" + expr + ")"
}

func searchFoldStatements() []string {
	return []string{
		`CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public`,
		`CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`,
		`CREATE EXTENSION IF NOT EXISTS btree_gin WITH SCHEMA public`,
		`CREATE OR REPLACE FUNCTION ` + SearchFoldFunction + `(text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
			AS $fold$ SELECT lower(public.unaccent('public.unaccent'::regdictionary, $1)) $fold$`,
	}
}

func CreateSearchFold(tx *gorm.DB) error {
	for _, sql := range searchFoldStatements() {
		if err := tx.Exec(sql).Error; err != nil {
			return err
		}
	}
	return nil
}
