package database

import (
	"strings"
	"testing"
)

func TestTheInvoiceProviderRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "inv_external_id_per_provider" {
			return
		}
	}
	t.Fatal("legacy invoices must get their provider and lose the global external id index at boot")
}

func TestTheExternalIdIsUniquePerProvider(t *testing.T) {
	sql, ok := PerformanceIndexSQL("ux_invoices_provider_external_id")
	if !ok {
		t.Fatal("ux_invoices_provider_external_id must be declared")
	}
	for _, part := range []string{"UNIQUE", "(provider, external_id)", "external_id IS NOT NULL"} {
		if !strings.Contains(sql, part) {
			t.Errorf("index must contain %q: %s", part, sql)
		}
	}
}

func TestInvoiceProviderRepairAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	for _, ddl := range []string{
		`DROP TABLE IF EXISTS invoices CASCADE`,
		`CREATE TABLE invoices (id text PRIMARY KEY, provider varchar(20) NOT NULL DEFAULT '', external_id varchar(255))`,
		`CREATE UNIQUE INDEX idx_invoices_external_id ON invoices (external_id)`,
		`INSERT INTO invoices (id, external_id) VALUES ('a', 'pay_abc123'), ('m', '1234567890'), ('e', ''), ('w', 'something-else')`,
		`INSERT INTO invoices (id, provider, external_id) VALUES ('kept', 'mercadopago', 'pay_looks_like_asaas')`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}

	for range 2 {
		if err := repairInvoiceExternalIDs(tx); err != nil {
			t.Fatalf("repair: %v", err)
		}
	}

	want := map[string]struct {
		provider string
		nullID   bool
	}{
		"a":    {"asaas", false},
		"m":    {"mercadopago", false},
		"e":    {"", true},
		"w":    {"", false},
		"kept": {"mercadopago", false},
	}
	for id, expected := range want {
		var row struct {
			Provider   string
			ExternalID *string
		}
		if err := tx.Raw(`SELECT provider, external_id FROM invoices WHERE id = ?`, id).Scan(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Provider != expected.provider || (row.ExternalID == nil) != expected.nullID {
			t.Errorf("%s: provider=%q nullID=%v, want %q nullID=%v", id, row.Provider, row.ExternalID == nil, expected.provider, expected.nullID)
		}
	}

	var legacyIndexes int64
	tx.Raw(`SELECT count(*) FROM pg_indexes WHERE tablename = 'invoices' AND indexname = 'idx_invoices_external_id'`).Scan(&legacyIndexes)
	if legacyIndexes != 0 {
		t.Fatal("the global external id index must be dropped so two providers can share an id")
	}
}
