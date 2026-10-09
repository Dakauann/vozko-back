package lead

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	volumeLeads          = 188000
	volumeNoiseLeads     = 200000
	volumeCampaigns      = 30
	volumeNoiseCampaigns = 200
	volumeNoisePerLead   = 15
)

const (
	volumeListBudget    = 300 * time.Millisecond
	volumeSectionBudget = 1500 * time.Millisecond
)

func volumeBudget(base time.Duration) time.Duration {
	scale, err := strconv.ParseFloat(os.Getenv("VOZKO_VOLUME_BUDGET_SCALE"), 64)
	if err != nil || scale <= 0 {
		return base
	}
	return time.Duration(float64(base) * scale)
}

func volumeTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&schema.LeadMemory{}, &schema.LeadMessageWindow{}, &schema.WhatsAppCampaign{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func volumeIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()
	declared := []struct {
		name   string
		lookup func(string) (string, bool)
	}{
		{"ux_leads_workspace_id_id", database.ConcurrentIndexSQL},
		{"idx_leads_workspace_owner", database.ConcurrentIndexSQL},
		{"idx_leads_workspace_birthday", database.ConcurrentIndexSQL},
		{"idx_leads_workspace_referred", database.ConcurrentIndexSQL},
		{"idx_lead_addresses_workspace_place", database.ConcurrentIndexSQL},
		{database.CampaignEntryLiveStatusIndex, database.ConcurrentIndexSQL},
		{"idx_leads_workspace_created", database.PerformanceIndexSQL},
		{"idx_lead_memories_lead", database.PerformanceIndexSQL},
		{"idx_lead_memories_lead_id", database.PerformanceIndexSQL},
		{database.LeadSearchNameIndex, database.ConcurrentIndexSQL},
		{database.LeadSearchNicknameIndex, database.ConcurrentIndexSQL},
		{database.LeadSearchNumberIndex, database.ConcurrentIndexSQL},
		{database.LeadPhoneSearchIndex, database.ConcurrentIndexSQL},
		{database.LeadMemorySearchIndex, database.ConcurrentIndexSQL},
		{database.LeadAddressPlaceLeadIndex, database.ConcurrentIndexSQL},
	}
	for _, idx := range declared {
		sql, ok := idx.lookup(idx.name)
		if !ok {
			t.Fatalf("index %s is not declared", idx.name)
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("index %s: %v", idx.name, err)
		}
	}
}

func seedVolume(t *testing.T, db *gorm.DB, ws, owner string) {
	t.Helper()
	workspaceLeads := "(SELECT id, workspace_id, row_number() OVER (ORDER BY id) AS n FROM leads WHERE workspace_id = '" + ws + "')"
	noiseLeads := "(SELECT id, workspace_id, row_number() OVER (ORDER BY id) AS n FROM leads WHERE workspace_id <> '" + ws + "')"
	steps := []string{
		`INSERT INTO leads (id, workspace_id, number, name, source, version, relatives_count, referred_count, birth_date, owner_id, owner_kind, blocked, custom_fields, created_at, updated_at)
		 SELECT gen_random_uuid(), '` + ws + `', '55119' || lpad(g::text, 8, '0'), 'Lead ' || g, 'import', 1, g % 3, g % 7,
		        CASE WHEN g % 10 = 0 THEN date '1960-01-01' + (g % 20000) END,
		        CASE WHEN g % 5 = 0 THEN '` + owner + `'::uuid END, CASE WHEN g % 5 = 0 THEN 'human' END,
		        g % 50 = 0,
		        CASE WHEN g % 2 = 0 THEN jsonb_build_object('interesse', (ARRAY['alto', 'medio', 'baixo'])[1 + g % 3]) END,
		        now() - (g || ' minutes')::interval, now()
		 FROM generate_series(1, ` + itoa(volumeLeads) + `) g`,
		`INSERT INTO leads (id, workspace_id, number, name, source, version, created_at, updated_at)
		 SELECT gen_random_uuid(), (ARRAY[gen_random_uuid()])[1], '55219' || lpad(g::text, 8, '0'), 'Noise ' || g, 'import', 1, now(), now()
		 FROM generate_series(1, ` + itoa(volumeNoiseLeads) + `) g`,
		`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, zip_code, district, district_key, city, city_key, state, geo_status, fingerprint, created_at, updated_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, 'home', true, 0, '0131' || lpad((l.n % 10000)::text, 4, '0'),
		        'Bairro ' || ((l.n / 7) % 70), 'bairro ' || ((l.n / 7) % 70),
		        (ARRAY['São Paulo', 'Contagem', 'Vespasiano'])[1 + l.n % 3], (ARRAY['sp:sao paulo', 'mg:contagem', 'mg:vespasiano'])[1 + l.n % 3],
		        (ARRAY['SP', 'MG', 'MG'])[1 + l.n % 3], 'pending', md5(l.id::text), now(), now()
		 FROM ` + workspaceLeads + ` l WHERE l.n % 5 < 2`,
		`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label, position, created_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, '55113' || lpad(l.n::text, 7, '0'), 'landline', 0, now()
		 FROM ` + workspaceLeads + ` l WHERE l.n % 10 = 0`,
		`INSERT INTO whatsapp_campaigns (id, workspace_id, name, created_at, updated_at)
		 SELECT gen_random_uuid(), '` + ws + `', 'Campanha ' || g, now(), now() FROM generate_series(1, ` + itoa(volumeCampaigns) + `) g`,
		`INSERT INTO whatsapp_campaigns (id, workspace_id, name, created_at, updated_at)
		 SELECT gen_random_uuid(), gen_random_uuid(), 'Ruido ' || g, now(), now() FROM generate_series(1, ` + itoa(volumeNoiseCampaigns) + `) g`,
		`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at)
		 SELECT gen_random_uuid(), c.ids[1 + (l.n + k) % ` + itoa(volumeCampaigns) + `], l.id, (ARRAY['SENT', 'DELIVERED', 'READ', 'FAILED'])[1 + (l.n + k) % 4], now(), now()
		 FROM ` + workspaceLeads + ` l
		 CROSS JOIN (SELECT array_agg(id ORDER BY id) AS ids FROM whatsapp_campaigns WHERE workspace_id = '` + ws + `') c
		 CROSS JOIN LATERAL generate_series(0, (l.n % 3)::int) k`,
		`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at)
		 SELECT gen_random_uuid(), c.ids[1 + (l.n + k * 13) % ` + itoa(volumeNoiseCampaigns) + `], l.id, (ARRAY['SENT', 'DELIVERED', 'READ', 'FAILED'])[1 + (l.n + k) % 4], now(), now()
		 FROM ` + noiseLeads + ` l
		 CROSS JOIN (SELECT array_agg(id ORDER BY id) AS ids FROM whatsapp_campaigns WHERE workspace_id <> '` + ws + `') c
		 CROSS JOIN generate_series(0, ` + itoa(volumeNoisePerLead-1) + `) k`,
		`INSERT INTO lead_memories (id, workspace_id, lead_id, category, content, content_norm, actor_kind, actor_id, created_at, updated_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, (ARRAY['interest', 'family', 'complaint'])[1 + l.n % 3], 'memória do lead ' || l.n, 'memoria do lead ' || l.n, 'ai', 'agent', now(), now()
		 FROM ` + workspaceLeads + ` l WHERE l.n % 4 = 0`,
		`INSERT INTO lead_memories (id, workspace_id, lead_id, category, content, content_norm, actor_kind, actor_id, created_at, updated_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, 'interest', 'ruído ' || l.n, 'ruido ' || l.n, 'ai', 'agent', now(), now()
		 FROM ` + noiseLeads + ` l WHERE l.n % 2 = 0`,
		`INSERT INTO lead_message_windows (id, lead_id, business_phone_id, last_message_at, created_at, updated_at)
		 SELECT gen_random_uuid(), l.id, gen_random_uuid(), now() - ((l.n % 48) || ' hours')::interval, now(), now()
		 FROM ` + workspaceLeads + ` l WHERE l.n % 6 = 0`,
		`INSERT INTO lead_message_windows (id, lead_id, business_phone_id, last_message_at, created_at, updated_at)
		 SELECT gen_random_uuid(), l.id, gen_random_uuid(), now() - ((l.n % 48) || ' hours')::interval, now(), now()
		 FROM ` + noiseLeads + ` l WHERE l.n % 2 = 0`,
		"VACUUM ANALYZE leads", "VACUUM ANALYZE lead_addresses", "VACUUM ANALYZE lead_phones", "VACUUM ANALYZE whatsapp_campaigns",
		"VACUUM ANALYZE whatsapp_campaign_entries", "VACUUM ANALYZE lead_memories", "VACUUM ANALYZE lead_message_windows",
	}
	for _, sql := range steps {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
}

func volumeDB(t *testing.T) (*gorm.DB, string, string) {
	t.Helper()
	db := leadStoreDB(t)
	volumeTables(t, db)
	ws, owner := uuid.NewString(), uuid.NewString()
	volumeIndexes(t, db)
	seedVolume(t, db, ws, owner)
	return db, ws, owner
}

func explain(t *testing.T, db *gorm.DB, sql string, args ...interface{}) (time.Duration, string) {
	t.Helper()
	var lines []string
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) "+sql, args...).Scan(&lines).Error; err != nil {
		t.Fatalf("explain: %v\n%s", err, sql)
	}
	var took time.Duration
	for _, line := range lines {
		if strings.HasPrefix(line, "Execution Time:") {
			ms := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "Execution Time:"), "ms"))
			took, _ = time.ParseDuration(ms + "ms")
		}
	}
	return took, strings.Join(lines, "\n")
}

func TestTheLeadListAndSectionsStayWithinBudgetOn188kLeadsAgainstPostgres(t *testing.T) {
	db, ws, owner := volumeDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	today := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	and := func(preds ...crmfilter.Predicate) crmfilter.Filter {
		return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: preds}}}
	}
	interest := crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "interesse", Operator: crmfilter.OpEquals, Values: []string{"alto"}}.BindKind(crmfilter.KindEnum)
	listBudget := volumeBudget(volumeListBudget)

	pages := []struct {
		name                     string
		filter                   crmfilter.Filter
		sorts                    []shared.Sort
		awaitsDenormalizedColumn bool
	}{
		{"newest first", crmfilter.Filter{}, nil, false},
		{"by relatives count", crmfilter.Filter{}, []shared.Sort{{Field: string(lead.SortRelatives), Direction: shared.SortDesc}}, false},
		{"by last activity", crmfilter.Filter{}, []shared.Sort{{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc}}, true},
		{"one bairro pair", and(crmfilter.Predicate{Field: crmfilter.FieldDistrict, Operator: crmfilter.OpIn, Values: []string{"mg:contagem/bairro 1"}}), nil, false},
		{"search with phones and memories", and(crmfilter.Predicate{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"lead 1234"}}), nil, false},
		{"birthdays this month", and(crmfilter.Predicate{Field: crmfilter.FieldBirthday, Operator: crmfilter.OpEquals, Values: []string{crmfilter.BirthdayThisMonth}}), nil, false},
		{"owner", and(crmfilter.Predicate{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIn, Values: []string{owner}}), nil, false},
		{"custom select value", and(interest), nil, false},
	}
	for _, p := range pages {
		input := lead.ListLeadsInput{WorkspaceID: ws, Filter: p.filter, Today: today,
			Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 2, PageSize: 200}, Sorts: p.sorts}}
		started := time.Now()
		page, err := repo.ListWithSummary(input)
		wall := time.Since(started)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		q, _ := repo.compile(input)
		idSQL, idArgs := q.pageIDs(input.Options)
		idTook, plan := explain(t, db, idSQL, idArgs...)
		var ids []string
		for _, item := range page.Items {
			ids = append(ids, item.Lead.ID)
		}
		sumTook := time.Duration(0)
		if len(ids) > 0 {
			sumSQL, sumArgs := q.pageSummaries(ids)
			sumTook, _ = explain(t, db, sumSQL, sumArgs...)
		}
		t.Logf("list %-32s total %6d page %3d wall %8s ids %8s summaries %8s", p.name, page.TotalItems, len(page.Items), wall.Round(time.Millisecond), idTook, sumTook)
		if idTook > listBudget && p.awaitsDenormalizedColumn {
			t.Logf("list %s reads its ids in %s, over the %s budget until last activity is a column", p.name, idTook, listBudget)
			continue
		}
		if idTook > listBudget {
			t.Errorf("list %s reads its ids in %s, over the %s budget\n%s", p.name, idTook, listBudget, plan)
		}
	}

	var expectedPair int64
	if err := db.Raw(`SELECT COUNT(*) FROM lead_addresses WHERE workspace_id = ? AND is_primary AND city_key = 'mg:contagem' AND district_key = 'bairro 1'`, ws).Scan(&expectedPair).Error; err != nil {
		t.Fatal(err)
	}
	pair, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: pages[3].filter})
	if err != nil || pair.TotalItems != expectedPair || expectedPair == 0 {
		t.Fatalf("district pair total = %d, %v; want %d", pair.TotalItems, err, expectedPair)
	}
	custom, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: and(interest)})
	if err != nil || custom.TotalItems != volumeLeads/6 {
		t.Fatalf("custom select total = %d, %v; want %d", custom.TotalItems, err, volumeLeads/6)
	}

	sectionBudget := volumeBudget(volumeSectionBudget)
	query := lead.SectionQuery{WorkspaceID: ws, Today: today}
	narrowed := lead.SectionQuery{WorkspaceID: ws, Today: today, Filter: pages[3].filter}
	sections := []struct {
		name  string
		run   func(lead.SectionQuery) error
		plans func(lead.SectionQuery) string
	}{
		{"summary", func(q lead.SectionQuery) error { _, err := repo.ReadSummary(ctx, q); return err }, nil},
		{"facets", func(q lead.SectionQuery) error { _, err := repo.ReadFacets(ctx, q); return err },
			func(q lead.SectionQuery) string { return facetPlans(t, repo, q) }},
		{"places", func(q lead.SectionQuery) error { _, err := repo.ReadPlaces(ctx, q); return err }, nil},
	}
	for _, s := range sections {
		for scope, q := range map[string]lead.SectionQuery{"whole workspace": query, "one bairro": narrowed} {
			started := time.Now()
			if err := s.run(q); err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}
			took := time.Since(started)
			t.Logf("section %-8s %-16s cold %s", s.name, scope, took.Round(time.Millisecond))
			if took <= sectionBudget {
				continue
			}
			plans := ""
			if s.plans != nil {
				plans = s.plans(q)
			}
			t.Errorf("section %s on the %s takes %s, over the %s cold budget\n%s", s.name, scope, took, sectionBudget, plans)
		}
	}

	summary, err := repo.ReadSummary(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	var windowOpen int64
	if err := db.Raw(`SELECT COUNT(DISTINCT w.lead_id) FROM lead_message_windows w JOIN leads l ON l.id = w.lead_id WHERE l.workspace_id = ? AND w.last_message_at > NOW() - INTERVAL '24 hours'`, ws).Scan(&windowOpen).Error; err != nil {
		t.Fatal(err)
	}
	if summary.Total != volumeLeads || summary.WithAddress != volumeLeads*2/5 || summary.Blocked != volumeLeads/50 || summary.WindowOpen != windowOpen || windowOpen == 0 {
		t.Fatalf("summary = %+v, want window open %d", summary, windowOpen)
	}
	places, err := repo.ReadPlaces(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(places.Cities) != 3 || len(places.Districts) != lead.MaxPlaceDistricts || places.Districts[0].Pair == "" {
		t.Fatalf("places = %d cities, %d districts", len(places.Cities), len(places.Districts))
	}
	facets, err := repo.ReadFacets(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Owners) != 1 || facets.Owners[0].Owner != owner || facets.Owners[0].Count != volumeLeads/5 || facets.OwnersTruncated || facets.Sources["import"] != volumeLeads {
		t.Fatalf("facets owners and sources = %+v", facets)
	}
	var expected []facetRow
	if err := db.Raw(`SELECT e.status AS key, COUNT(DISTINCT e.lead_id) AS count FROM whatsapp_campaign_entries e JOIN leads l ON l.id = e.lead_id WHERE l.workspace_id = ? GROUP BY e.status`, ws).Scan(&expected).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range expected {
		if facets.CampaignStatuses[row.Key] != row.Count {
			t.Fatalf("campaign status %s = %d, want %d", row.Key, facets.CampaignStatuses[row.Key], row.Count)
		}
	}
	if facets.Channels["whatsapp"] == 0 || facets.MemoryCategories["interest"] != volumeLeads/12 {
		t.Fatalf("facets channels and memories = %+v, %+v", facets.Channels, facets.MemoryCategories)
	}
}

func TestExplainTheLeadSummaryOn188kLeadsAgainstPostgres(t *testing.T) {
	db, ws, _ := volumeDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	q, err := repo.compile(lead.ListLeadsInput{WorkspaceID: ws, Today: time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	sql, args, err := q.summarySQL()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		took, plan := explain(t, db, sql, args...)
		t.Logf("summary run %d %s\n%s", i, took, plan)
	}
}

func facetPlans(t *testing.T, repo *repository, sq lead.SectionQuery) string {
	t.Helper()
	q, err := repo.sectionQuery(sq)
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		name string
		read func() (string, []interface{})
	}{
		{"campaign statuses", q.campaignStatusesSQL},
		{"channels", q.channelsSQL},
		{"memory categories", q.memoryCategoriesSQL},
		{"lead columns", func() (string, []interface{}) { return q.leadColumnsSQL(sq.ClassificationKey) }},
	}
	var report strings.Builder
	err = repo.inSection(context.Background(), q, func(tx *gorm.DB) error {
		for _, s := range statements {
			sql, args := s.read()
			took, plan := explain(t, tx, sql, args...)
			report.WriteString("facet " + s.name + " " + took.String() + "\n" + plan + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return report.String()
}
