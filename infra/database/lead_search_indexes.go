package database

const (
	LeadSearchNameIndex       = "idx_leads_search_name"
	LeadSearchNicknameIndex   = "idx_leads_search_nickname"
	LeadSearchNumberIndex     = "idx_leads_search_number"
	LeadPhoneSearchIndex      = "idx_lead_phones_search_number"
	LeadMemorySearchIndex     = "idx_lead_memories_search_content"
	LeadAddressPlaceLeadIndex = "idx_lead_addresses_place_lead"
)

func workspaceTrigram(name, table, expr, where string) concurrentIndex {
	sql := `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + name + `
				ON ` + table + ` USING gin (workspace_id, ` + expr + ` public.gin_trgm_ops)`
	if where != "" {
		sql += `
				WHERE ` + where
	}
	return concurrentIndex{name: name, sql: sql}
}

func leadSearchIndexes() []concurrentIndex {
	return []concurrentIndex{
		workspaceTrigram(LeadSearchNameIndex, "leads", SearchFold("name"), "deleted_at IS NULL"),
		workspaceTrigram(LeadSearchNicknameIndex, "leads", SearchFold("nickname"), "nickname IS NOT NULL AND deleted_at IS NULL"),
		workspaceTrigram(LeadSearchNumberIndex, "leads", "number", "deleted_at IS NULL"),
		workspaceTrigram(LeadPhoneSearchIndex, "lead_phones", "number", ""),
		workspaceTrigram(LeadMemorySearchIndex, "lead_memories", SearchFold("content"), "deleted_at IS NULL"),
		{
			name: LeadAddressPlaceLeadIndex,
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + LeadAddressPlaceLeadIndex + `
				ON lead_addresses (workspace_id, city_key, district_key) INCLUDE (lead_id)
				WHERE is_primary`,
		},
	}
}
