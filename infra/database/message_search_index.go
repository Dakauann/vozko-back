package database

import "vozko/infra/database/sqlexpr"

const MessageTextSearchIndex = "idx_cm_search_text_trgm"

func messageSearchIndexes() []ddl {
	return []ddl{{
		name: MessageTextSearchIndex,
		sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + MessageTextSearchIndex + `
				ON conversation_messages USING gin ((` + sqlexpr.MessageSearchText("") + `) public.gin_trgm_ops)
				WHERE ` + sqlexpr.SearchableMessage(""),
	}}
}
