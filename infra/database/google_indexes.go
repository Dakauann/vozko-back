package database

func googlePerformanceIndexes() []ddl {
	return append(googleAccountPerformanceIndexes(), googleSheetPerformanceIndexes()...)
}

func googleSchemaConstraints() []ddl {
	return append(googleAccountSchemaConstraints(), googleSheetSchemaConstraints()...)
}

func googleAccountPerformanceIndexes() []ddl {
	return []ddl{
		{
			name: "idx_google_acct_refresh_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_acct_refresh_due
				ON google_accounts (access_expires_at, id)
				WHERE status = 'connected'`,
		},
		{
			name: "idx_google_acct_email",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_acct_email
				ON google_accounts (email)
				WHERE status <> 'disconnected'`,
		},
	}
}

func googleAccountSchemaConstraints() []ddl {
	return []ddl{
		{
			name: "ux_google_account_workspace_email",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_google_account_workspace_email
				ON google_accounts (workspace_id, email)`,
		},
	}
}

func googleSheetPerformanceIndexes() []ddl {
	return []ddl{
		{
			name: "idx_google_sheet_ws_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_ws_created
				ON google_sheets (workspace_id, created_at DESC, id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_google_sheet_rows_claim",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_claim
				ON google_sheet_rows (sheet_id, created_at, id)
				WHERE status = 'pending'`,
		},
		{
			name: "idx_google_sheet_rows_ready",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_ready
				ON google_sheet_rows (next_attempt_at, sheet_id)
				WHERE status = 'pending'`,
		},
		{
			name: "idx_google_sheet_rows_in_flight",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_in_flight
				ON google_sheet_rows (sheet_id, claimed_at)
				WHERE status = 'in_flight'`,
		},
		{
			name: "idx_google_sheet_rows_problems",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_problems
				ON google_sheet_rows (sheet_id, created_at DESC)
				WHERE status IN ('held', 'failed')`,
		},
		{
			name: "idx_google_sheet_rows_written",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_written
				ON google_sheet_rows (sheet_id, written_at)
				WHERE status = 'written'`,
		},
		{
			name: "idx_google_sheet_rows_purge",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_purge
				ON google_sheet_rows (written_at)
				WHERE status = 'written'`,
		},
		{
			name: "idx_google_sheet_rows_lead",
			sql: `CREATE INDEX IF NOT EXISTS idx_google_sheet_rows_lead
				ON google_sheet_rows (workspace_id, lead_id)
				WHERE lead_id IS NOT NULL`,
		},
	}
}

func googleSheetSchemaConstraints() []ddl {
	return []ddl{
		{
			name: "ux_google_sheet_tab",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_google_sheet_tab
				ON google_sheets (workspace_id, spreadsheet_id, tab_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_google_sheet_rows_key",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_google_sheet_rows_key
				ON google_sheet_rows (sheet_id, idempotency_key)`,
		},
	}
}
