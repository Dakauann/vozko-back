package lead

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

func TestCountAndMembershipFiltersMatchThePerLeadCountsAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	volumeTables(t, db)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ws, other := uuid.NewString(), uuid.NewString()
	steps := []string{
		`INSERT INTO leads (id, workspace_id, name, source, version, created_at, updated_at)
		 SELECT gen_random_uuid(), CASE WHEN g <= 8 THEN '` + ws + `'::uuid ELSE '` + other + `'::uuid END, 'Lead ' || g, 'import', 1, now(), now() FROM generate_series(1, 12) g`,
		`INSERT INTO whatsapp_campaigns (id, workspace_id, name, created_at, updated_at)
		 SELECT gen_random_uuid(), w, 'Campanha ' || k, now(), now() FROM (VALUES ('` + ws + `'::uuid), ('` + other + `'::uuid)) v(w), generate_series(1, 3) k`,
		`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at, deleted_at)
		 SELECT gen_random_uuid(), c.id, l.id, CASE WHEN c.n = 1 THEN 'READ' ELSE 'FAILED' END, now(), now(), CASE WHEN l.n = 7 AND c.n = 2 THEN now() END
		 FROM (SELECT id, workspace_id, row_number() OVER (PARTITION BY workspace_id ORDER BY id) AS n FROM leads) l
		 JOIN (SELECT id, workspace_id, row_number() OVER (PARTITION BY workspace_id ORDER BY name) AS n FROM whatsapp_campaigns) c
		   ON c.workspace_id = l.workspace_id AND c.n <= l.n % 4`,
		`INSERT INTO lead_memories (id, workspace_id, lead_id, category, content, content_norm, actor_kind, actor_id, created_at, updated_at, deleted_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, CASE WHEN k = 1 THEN 'deal' ELSE 'objection' END, 'nota ' || k, 'nota ' || l.id || k,
		        CASE WHEN k = 1 THEN 'human' ELSE 'ai' END, 'agent', now(), now(), CASE WHEN l.n = 6 AND k = 2 THEN now() END
		 FROM (SELECT id, workspace_id, row_number() OVER (PARTITION BY workspace_id ORDER BY id) AS n FROM leads) l
		 CROSS JOIN LATERAL generate_series(1, (l.n % 3)::int) k`,
	}
	for _, sql := range steps {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	perLead := func(condition string, args ...interface{}) int64 {
		var n int64
		sql := `SELECT COUNT(*) FROM leads l WHERE l.workspace_id = ? AND l.deleted_at IS NULL AND ` + condition
		if err := db.Raw(sql, append([]interface{}{ws}, args...)...).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	campaigns := "(SELECT COUNT(*) FROM whatsapp_campaign_entries e WHERE e.lead_id = l.id AND e.deleted_at IS NULL)"
	memories := "(SELECT COUNT(*) FROM lead_memories m WHERE m.lead_id = l.id AND m.deleted_at IS NULL)"
	cases := []struct {
		name string
		pred crmfilter.Predicate
		want int64
	}{
		{"no campaign", crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpEquals, Values: []string{"0"}}, perLead(campaigns + " = 0")},
		{"two campaigns", crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpEquals, Values: []string{"2"}}, perLead(campaigns + " = 2")},
		{"not two campaigns", crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpNotEquals, Values: []string{"2"}}, perLead(campaigns + " <> 2")},
		{"campaigns 2 to 3", crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpBetween, Values: []string{"2", "3"}}, perLead(campaigns + " BETWEEN 2 AND 3")},
		{"campaigns 0 to 1", crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpBetween, Values: []string{"0", "1"}}, perLead(campaigns + " BETWEEN 0 AND 1")},
		{"at most one campaign", crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpLessEq, Values: []string{"1"}}, perLead(campaigns + " <= 1")},
		{"at least one memory", crmfilter.Predicate{Field: crmfilter.FieldMemoryCount, Operator: crmfilter.OpGreaterEq, Values: []string{"1"}}, perLead(memories + " >= 1")},
		{"at most one memory", crmfilter.Predicate{Field: crmfilter.FieldMemoryCount, Operator: crmfilter.OpLessEq, Values: []string{"1"}}, perLead(memories + " <= 1")},
		{"memory count set", crmfilter.Predicate{Field: crmfilter.FieldMemoryCount, Operator: crmfilter.OpIsSet}, perLead("true")},
		{"memory count empty", crmfilter.Predicate{Field: crmfilter.FieldMemoryCount, Operator: crmfilter.OpIsEmpty}, 0},
		{"read", crmfilter.Predicate{Field: crmfilter.FieldCampaignStatus, Operator: crmfilter.OpIn, Values: []string{"READ"}},
			perLead("EXISTS (SELECT 1 FROM whatsapp_campaign_entries e WHERE e.lead_id = l.id AND e.deleted_at IS NULL AND e.status = 'READ')")},
		{"never failed", crmfilter.Predicate{Field: crmfilter.FieldCampaignStatus, Operator: crmfilter.OpNotIn, Values: []string{"FAILED"}},
			perLead("NOT EXISTS (SELECT 1 FROM whatsapp_campaign_entries e WHERE e.lead_id = l.id AND e.deleted_at IS NULL AND e.status = 'FAILED')")},
		{"a deal memory", crmfilter.Predicate{Field: crmfilter.FieldMemoryCategory, Operator: crmfilter.OpIn, Values: []string{"deal"}},
			perLead("EXISTS (SELECT 1 FROM lead_memories m WHERE m.lead_id = l.id AND m.deleted_at IS NULL AND m.category = 'deal')")},
		{"no ai memory", crmfilter.Predicate{Field: crmfilter.FieldMemoryAuthor, Operator: crmfilter.OpNotIn, Values: []string{"ai"}},
			perLead("NOT EXISTS (SELECT 1 FROM lead_memories m WHERE m.lead_id = l.id AND m.deleted_at IS NULL AND m.actor_kind = 'ai')")},
	}
	for _, tc := range cases {
		page, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{tc.pred}}}}})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if page.TotalItems != tc.want {
			t.Errorf("%s = %d leads, want %d", tc.name, page.TotalItems, tc.want)
		}
	}
}
