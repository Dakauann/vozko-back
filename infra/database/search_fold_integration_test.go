package database

import (
	"testing"

	"vozko/infra/repositories/repotest"
)

func TestTheSearchFoldIgnoresAccentsAndCaseAndCanBeIndexedAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "search_fold")
	if err := CreateSearchFold(db); err != nil {
		t.Fatalf("CreateSearchFold: %v", err)
	}
	if err := CreateSearchFold(db); err != nil {
		t.Fatalf("a second boot must keep the fold: %v", err)
	}
	var folded string
	if err := db.Raw(`SELECT ` + SearchFold(`'Santo ANTÔNIO, João, Conceição'`)).Scan(&folded).Error; err != nil {
		t.Fatal(err)
	}
	if folded != "santo antonio, joao, conceicao" {
		t.Fatalf("folded = %q", folded)
	}
	for _, sql := range []string{
		`CREATE TABLE fold_rows (id int PRIMARY KEY, name text)`,
		`CREATE INDEX fold_rows_name ON fold_rows USING gin (` + SearchFold("name") + ` public.gin_trgm_ops)`,
		`INSERT INTO fold_rows VALUES (1, 'Antônio Silva'), (2, 'Maria')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	var ids []int
	if err := db.Raw(`SELECT id FROM fold_rows WHERE `+SearchFold("name")+` LIKE `+SearchFold("?"), "%ANTONIO%").Scan(&ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("ids = %v, want [1]", ids)
	}
}
