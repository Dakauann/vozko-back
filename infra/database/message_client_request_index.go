package database

const MessageClientRequestIndex = "ux_cm_entry_client_request"

func messageClientRequestIndexes() []ddl {
	return []ddl{{
		name: MessageClientRequestIndex,
		sql: `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ` + MessageClientRequestIndex + `
				ON conversation_messages (entry_type, entry_id, client_request_id)
				WHERE client_request_id IS NOT NULL`,
	}}
}
