package database

import (
	"strings"
	"testing"
)

func TestEveryLeadSearchColumnHasAWorkspaceTrigramIndex(t *testing.T) {
	cases := map[string][]string{
		LeadSearchNameIndex:       {"ON leads USING gin (workspace_id, " + SearchFold("name") + " public.gin_trgm_ops)", "WHERE deleted_at IS NULL"},
		LeadSearchNicknameIndex:   {"ON leads USING gin (workspace_id, " + SearchFold("nickname") + " public.gin_trgm_ops)", "WHERE nickname IS NOT NULL AND deleted_at IS NULL"},
		LeadSearchNumberIndex:     {"ON leads USING gin (workspace_id, number public.gin_trgm_ops)", "WHERE deleted_at IS NULL"},
		LeadPhoneSearchIndex:      {"ON lead_phones USING gin (workspace_id, number public.gin_trgm_ops)"},
		LeadMemorySearchIndex:     {"ON lead_memories USING gin (workspace_id, " + SearchFold("content") + " public.gin_trgm_ops)", "WHERE deleted_at IS NULL"},
		LeadAddressPlaceLeadIndex: {"ON lead_addresses (workspace_id, city_key, district_key) INCLUDE (lead_id)", "WHERE is_primary"},
	}
	for name, fragments := range cases {
		sql, ok := ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not declared", name)
		}
		flat := strings.Join(strings.Fields(sql), " ")
		for _, fragment := range fragments {
			if !strings.Contains(flat, fragment) {
				t.Errorf("%s misses %q: %s", name, fragment, flat)
			}
		}
	}
}
