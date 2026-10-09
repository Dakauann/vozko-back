package database

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

const leadKeyLockTimeout = "5s"

type compositeLeadKey struct {
	table      string
	constraint string
	column     string
}

var compositeLeadKeys = []compositeLeadKey{
	{table: "lead_phones", constraint: "fk_lead_phones_workspace_lead", column: "lead_id"},
	{table: "lead_addresses", constraint: "fk_lead_addresses_workspace_lead", column: "lead_id"},
	{table: "lead_relations", constraint: "fk_lead_relations_workspace_lead", column: "lead_id"},
	{table: "lead_relations", constraint: "fk_lead_relations_workspace_other_lead", column: "other_lead_id"},
	{table: "call_list_items", constraint: "fk_lead_call_list_items_workspace_lead", column: "lead_id"},
}

func leadCollectionConstraints() []struct{ name, sql string } {
	return []struct{ name, sql string }{
		{
			name: "ux_lead_addresses_primary",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_lead_addresses_primary
				ON lead_addresses (lead_id)
				WHERE is_primary`,
		},
		{
			name: "ux_lead_relations_pair",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_lead_relations_pair
				ON lead_relations (workspace_id, least(lead_id, other_lead_id), greatest(lead_id, other_lead_id), dimension)`,
		},
		{
			name: "idx_lead_relations_lead",
			sql:  `DROP INDEX IF EXISTS idx_lead_relations_lead`,
		},
		{
			name: "idx_lead_relations_other_lead",
			sql:  `DROP INDEX IF EXISTS idx_lead_relations_other_lead`,
		},
	}
}

func CreateLeadCollectionConstraints(db *gorm.DB) error {
	for _, c := range leadCollectionConstraints() {
		if err := db.Exec(c.sql).Error; err != nil {
			return fmt.Errorf("creating constraint %s: %w", c.name, err)
		}
	}
	return nil
}

func (k compositeLeadKey) addSQL() string {
	return fmt.Sprintf(`DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = '%[2]s' AND conrelid = '%[1]s'::regclass) THEN
				ALTER TABLE %[1]s ADD CONSTRAINT %[2]s FOREIGN KEY (workspace_id, %[3]s)
					REFERENCES leads (workspace_id, id) ON DELETE CASCADE NOT VALID;
			END IF;
		END $$`, k.table, k.constraint, k.column)
}

func EnsureCompositeLeadKeys(db *gorm.DB) error {
	for _, key := range compositeLeadKeys {
		if err := ensureCompositeLeadKey(db, key); err != nil {
			return fmt.Errorf("composite key %s: %w", key.constraint, err)
		}
	}
	return nil
}

func ensureCompositeLeadKey(db *gorm.DB, key compositeLeadKey) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('lock_timeout', ?, true)", leadKeyLockTimeout).Error; err != nil {
			return err
		}
		return tx.Exec(key.addSQL()).Error
	})
	if err != nil {
		return err
	}
	var pending bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = ? AND conrelid = ?::regclass AND NOT convalidated)", key.constraint, key.table).Scan(&pending).Error; err != nil {
		return err
	}
	if !pending {
		return nil
	}
	return db.Exec(fmt.Sprintf("ALTER TABLE %s VALIDATE CONSTRAINT %s", key.table, key.constraint)).Error
}

func createCompositeLeadKeys(db *gorm.DB) {
	if err := EnsureCompositeLeadKeys(db); err != nil {
		log.Printf("[indexes] Warning: %v; it is retried on the next boot", err)
	}
}
