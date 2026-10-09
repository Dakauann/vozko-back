package database

import (
	"fmt"

	"gorm.io/gorm"
)

func callListConstraints() []struct{ name, sql string } {
	return []struct{ name, sql string }{
		{
			name: "fk_call_list_items_workspace_list",
			sql: `DO $$
				BEGIN
					IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_call_list_items_workspace_list' AND conrelid = 'call_list_items'::regclass) THEN
						ALTER TABLE call_list_items ADD CONSTRAINT fk_call_list_items_workspace_list FOREIGN KEY (workspace_id, list_id) REFERENCES call_lists (workspace_id, id) ON DELETE CASCADE;
					END IF;
				END $$`,
		},
		{
			name: "fk_call_list_items_last_call",
			sql: `DO $$
				BEGIN
					IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_call_list_items_last_call' AND conrelid = 'call_list_items'::regclass) THEN
						ALTER TABLE call_list_items ADD CONSTRAINT fk_call_list_items_last_call FOREIGN KEY (last_call_id) REFERENCES calls (id) ON DELETE SET NULL;
					END IF;
				END $$`,
		},
	}
}

func CreateCallListConstraints(db *gorm.DB) error {
	for _, c := range callListConstraints() {
		if err := db.Exec(c.sql).Error; err != nil {
			return fmt.Errorf("creating constraint %s: %w", c.name, err)
		}
	}
	return nil
}
