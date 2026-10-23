package database

import (
	"fmt"

	"vozko/domain/payment"

	"gorm.io/gorm"
)

func repairInvoiceExternalIDs(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("invoices") {
		return nil
	}
	steps := []struct {
		sql  string
		args []interface{}
	}{
		{`ALTER TABLE invoices DROP CONSTRAINT IF EXISTS uni_invoices_external_id`, nil},
		{`DROP INDEX IF EXISTS idx_invoices_external_id`, nil},
		{`UPDATE invoices SET external_id = NULL WHERE external_id = ''`, nil},
		{`UPDATE invoices SET provider = ? WHERE provider = '' AND external_id LIKE 'pay\_%'`, []interface{}{string(payment.ProviderAsaas)}},
		{`UPDATE invoices SET provider = ? WHERE provider = '' AND external_id ~ '^[0-9]+$'`, []interface{}{string(payment.ProviderMercadoPago)}},
	}
	for _, step := range steps {
		if err := tx.Exec(step.sql, step.args...).Error; err != nil {
			return fmt.Errorf("%s: %w", step.sql, err)
		}
	}
	return nil
}
