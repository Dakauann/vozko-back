package database

func tiktokPerformanceIndexes() []ddl {
	return []ddl{
		{
			name: "idx_tt_acct_workspace_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_acct_workspace_created
				ON tiktok_accounts (workspace_id, created_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_tt_acct_refresh_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_acct_refresh_due
				ON tiktok_accounts (access_expires_at, id)
				WHERE status = 'CONNECTED' AND deleted_at IS NULL`,
		},
		{
			name: "idx_tt_acct_health_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_acct_health_due
				ON tiktok_accounts (last_health_check_at, id)
				WHERE status IN ('CONNECTED', 'NEEDS_RECONNECT') AND deleted_at IS NULL`,
		},
		{
			name: "idx_tt_acct_purge_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_acct_purge_due
				ON tiktok_accounts (purge_after, id)
				WHERE purge_after IS NOT NULL`,
		},
		{
			name: "idx_tt_conv_workspace_lastmsg",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_conv_workspace_lastmsg
				ON tiktok_conversations (workspace_id, last_message_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_tt_conv_account_lastmsg",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_conv_account_lastmsg
				ON tiktok_conversations (account_id, last_message_at DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_tt_conv_account_created",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_conv_account_created
				ON tiktok_conversations (account_id, created_at DESC, id DESC)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "idx_tt_video_account_posted",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_video_account_posted
				ON tiktok_videos (account_id, posted_at DESC, item_id DESC)`,
		},
		{
			name: "idx_tt_comment_video_time",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_comment_video_time
				ON tiktok_comments (account_id, video_id, commented_at DESC, comment_id DESC)`,
		},
		{
			name: "idx_tt_comment_work_due",
			sql: `CREATE INDEX IF NOT EXISTS idx_tt_comment_work_due
				ON tiktok_comments (next_lookup_at, id)
				WHERE lookup = 'pending' OR rules_due`,
		},
	}
}

func tiktokSchemaConstraints() []ddl {
	return []ddl{
		{
			name: "ux_tt_account_live_open_id",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tt_account_live_open_id
				ON tiktok_accounts (open_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_tt_contact_account_person",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tt_contact_account_person
				ON tiktok_contacts (account_id, unique_identifier)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_tt_conversation_account_thread",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tt_conversation_account_thread
				ON tiktok_conversations (account_id, conversation_id)
				WHERE deleted_at IS NULL`,
		},
		{
			name: "ux_tt_video_account_item",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tt_video_account_item
				ON tiktok_videos (account_id, item_id)`,
		},
		{
			name: "ux_tt_comment_account_comment",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS ux_tt_comment_account_comment
				ON tiktok_comments (account_id, comment_id)`,
		},
	}
}

func TikTokDDLStatements() []string {
	all := append(tiktokSchemaConstraints(), tiktokPerformanceIndexes()...)
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.sql
	}
	return out
}
