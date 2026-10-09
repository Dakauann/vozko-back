package lead

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	leadarea_repository "vozko/infra/repositories/leadarea"
	"vozko/infra/repositories/repotest"
)

const (
	mapBudgetLeads           = 188000
	mapBudgetNoiseLeads      = 1512000
	mapBudgetNoiseWorkspaces = 400
	mapBudgetCampaigns       = 30
	mapBudgetNoiseCampaigns  = 200
	mapBudgetRuns            = 5
	mapBudgetWarmMS          = 300.0
	mapBudgetTileWarmMS      = 150.0
	mapBudgetColdMS          = 1500.0
)

type mapBudgetFixture struct {
	db        *gorm.DB
	ws        string
	owners    []string
	campaigns []string
	stages    []string
	labels    []string
	areas     leadarea.Repository
	viewer    string
	city      string
	small     string
}

func mapBudgetNoiseWorkspace(expr string) string {
	return "('00000000-0000-0000-0000-' || lpad((" + expr + " % " + itoa(mapBudgetNoiseWorkspaces) + ")::text, 12, '0'))::uuid"
}

func newUUIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuid.NewString()
	}
	return out
}

func mapBudgetIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()
	declared := []struct {
		name   string
		lookup func(string) (string, bool)
	}{
		{database.LeadLiveIndex, database.ConcurrentIndexSQL},
		{"idx_wa_campaign_ws_del", database.PerformanceIndexSQL},
		{"idx_wce_campaign_del", database.PerformanceIndexSQL},
		{"idx_entry_stages_stage_entry", database.PerformanceIndexSQL},
		{"idx_entry_stages_entry", database.PerformanceIndexSQL},
		{"idx_entry_stages_ws_del", database.PerformanceIndexSQL},
		{"idx_entry_stages_stage_ws_del", database.PerformanceIndexSQL},
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

func mapBudgetDB(t *testing.T) *gorm.DB {
	t.Helper()
	if kept := os.Getenv("VOZKO_MAP_BUDGET_SCHEMA"); kept != "" {
		return repotest.KeptDB(t, kept)
	}
	return repotest.IsolatedDB(t, "lead_map")
}

func openMapBudget(t *testing.T) mapBudgetFixture {
	t.Helper()
	db := mapBudgetDB(t)
	mapTables(t, db)
	volumeTables(t, db)
	if err := db.AutoMigrate(&schema.EntryStage{}, &schema.EntryLabel{}); err != nil {
		t.Fatalf("migrate entry stages and labels: %v", err)
	}
	var areas int64
	if err := db.Raw(`SELECT COUNT(*) FROM lead_areas`).Scan(&areas).Error; err != nil {
		t.Fatal(err)
	}
	if areas == 0 {
		seedMapBudget(t, db)
	}
	volumeIndexes(t, db)
	mapBudgetIndexes(t, db)
	return loadMapBudget(t, db)
}

func loadMapBudget(t *testing.T, db *gorm.DB) mapBudgetFixture {
	t.Helper()
	f := mapBudgetFixture{db: db, areas: leadarea_repository.NewRepository(db), viewer: uuid.NewString()}
	reads := []struct {
		sql  string
		into interface{}
	}{
		{`SELECT workspace_id::text FROM lead_areas WHERE name = 'São Paulo'`, &f.ws},
		{`SELECT id::text FROM lead_areas WHERE name = 'São Paulo'`, &f.city},
		{`SELECT id::text FROM lead_areas WHERE name = 'Paulista'`, &f.small},
		{`SELECT DISTINCT owner_id::text FROM leads WHERE workspace_id = (SELECT workspace_id FROM lead_areas LIMIT 1) AND owner_id IS NOT NULL ORDER BY 1`, &f.owners},
		{`SELECT id::text FROM whatsapp_campaigns WHERE workspace_id = (SELECT workspace_id FROM lead_areas LIMIT 1) ORDER BY id`, &f.campaigns},
		{`SELECT DISTINCT stage_id::text FROM entry_stages WHERE workspace_id = (SELECT workspace_id FROM lead_areas LIMIT 1) ORDER BY 1`, &f.stages},
		{`SELECT DISTINCT label_id::text FROM entry_labels WHERE workspace_id = (SELECT workspace_id FROM lead_areas LIMIT 1) ORDER BY 1`, &f.labels},
	}
	for _, r := range reads {
		if err := db.Raw(r.sql).Scan(r.into).Error; err != nil {
			t.Fatal(err)
		}
	}
	if f.ws == "" || f.city == "" || f.small == "" || len(f.owners) == 0 || len(f.campaigns) < 2 || len(f.stages) == 0 || len(f.labels) == 0 {
		t.Fatalf("the map budget fixture is incomplete: %+v", f)
	}
	return f
}

func seedMapBudget(t *testing.T, db *gorm.DB) {
	t.Helper()
	f := mapBudgetFixture{ws: uuid.NewString(), owners: newUUIDs(4), campaigns: newUUIDs(mapBudgetCampaigns), stages: newUUIDs(6), labels: newUUIDs(8), viewer: uuid.NewString()}
	ws := f.ws
	mine := "(SELECT id, workspace_id, row_number() OVER (ORDER BY id) AS n FROM leads WHERE workspace_id = '" + ws + "')"
	noise := "(SELECT id, workspace_id, abs(hashtext(id::text)) AS h FROM leads WHERE workspace_id <> '" + ws + "')"
	noiseEntries := "(SELECT e.id, c.workspace_id, abs(hashtext(e.id::text)) AS h FROM whatsapp_campaign_entries e JOIN whatsapp_campaigns c ON c.id = e.campaign_id WHERE c.workspace_id <> '" + ws + "')"
	mineEntries := "(SELECT e.id, abs(hashtext(e.id::text)) AS h FROM whatsapp_campaign_entries e JOIN whatsapp_campaigns c ON c.id = e.campaign_id WHERE c.workspace_id = '" + ws + "')"
	steps := []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO leads (id, workspace_id, number, name, nickname, source, version, owner_id, owner_kind, blocked, custom_fields, created_at, updated_at)
		 SELECT gen_random_uuid(), CASE WHEN g <= ` + itoa(mapBudgetLeads) + ` THEN '` + ws + `'::uuid ELSE ` + mapBudgetNoiseWorkspace("g") + ` END,
		        '558499' || lpad(g::text, 7, '0'),
		        f[1 + (g::bigint * 7919) % 60] || ' ' || s[1 + (g::bigint * 104729) % 38] || ' ' || s[1 + (g::bigint * 15485863) % 38],
		        CASE WHEN g % 10 = 0 THEN 'Apelido ' || (g % 997) END, 'import', 1,
		        CASE WHEN g <= ` + itoa(mapBudgetLeads) + ` AND g % 5 = 0 THEN (?::uuid[])[1 + (g / 5) % 4] END,
		        CASE WHEN g <= ` + itoa(mapBudgetLeads) + ` AND g % 5 = 0 THEN 'human' END,
		        g % 50 = 0,
		        jsonb_build_object('classificacao', (ARRAY['Positivo', 'Negativo', 'A conquistar', 'Não informado'])[1 + g % 4]),
		        now() - (g || ' seconds')::interval, now()
		 FROM generate_series(1, ` + itoa(mapBudgetLeads+mapBudgetNoiseLeads) + `) g, (SELECT ` + searchFirstNames + ` AS f, ` + searchSurnames + ` AS s) names`,
			[]interface{}{pq.Array(f.owners)}},
		{`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, city, city_key, district, district_key, state,
		        latitude, longitude, geo_precision, geo_status, fingerprint, created_at, updated_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, 'home', true, 0, 'Natal', 'rn:natal', 'Bairro ' || (l.h % 300), 'bairro ' || (l.h % 300), 'RN',
		        CASE WHEN l.h % 4 < 3 THEN round((-5.79 + ((l.h % 1000) / 1000.0 - 0.5) * 0.3)::numeric, 4)::float8 END,
		        CASE WHEN l.h % 4 < 3 THEN round((-35.21 + (((l.h / 1000) % 1000) / 1000.0 - 0.5) * 0.3)::numeric, 4)::float8 END,
		        CASE WHEN l.h % 4 < 3 THEN 'street' END, CASE WHEN l.h % 4 < 3 THEN 'located' ELSE 'pending' END,
		        left(md5(l.id::text), 32), now(), now()
		 FROM ` + noise + ` l WHERE l.h % 5 < 2`, nil},
		{`INSERT INTO whatsapp_campaigns (id, workspace_id, name, created_at, updated_at)
		 SELECT c.id, '` + ws + `', 'Campanha ' || c.n, now(), now() FROM unnest(?::uuid[]) WITH ORDINALITY c(id, n)`, []interface{}{pq.Array(f.campaigns)}},
		{`INSERT INTO whatsapp_campaigns (id, workspace_id, name, created_at, updated_at)
		 SELECT gen_random_uuid(), ` + mapBudgetNoiseWorkspace("g") + `, 'Ruido ' || g, now(), now() FROM generate_series(1, ` + itoa(mapBudgetNoiseCampaigns) + `) g`, nil},
		{`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at)
		 SELECT gen_random_uuid(), c.ids[1 + (l.n + k * 7) % ` + itoa(mapBudgetCampaigns) + `], l.id, (ARRAY['SENT', 'DELIVERED', 'READ', 'FAILED'])[1 + (l.n + k) % 4], now(), now()
		 FROM ` + mine + ` l
		 CROSS JOIN (SELECT ?::uuid[] AS ids) c
		 CROSS JOIN LATERAL generate_series(1, (l.n % 4)::int) k`, []interface{}{pq.Array(f.campaigns)}},
		{`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at)
		 SELECT gen_random_uuid(), c.ids[1 + (l.h + k * 13) % ` + itoa(mapBudgetNoiseCampaigns) + `], l.id, (ARRAY['SENT', 'DELIVERED', 'READ', 'FAILED'])[1 + (l.h + k) % 4], now(), now()
		 FROM ` + noise + ` l
		 CROSS JOIN (SELECT array_agg(id ORDER BY id) AS ids FROM whatsapp_campaigns WHERE workspace_id <> '` + ws + `') c
		 CROSS JOIN generate_series(0, 1) k`, nil},
		{`INSERT INTO entry_stages (id, stage_id, entry_id, entry_type, workspace_id, created_at)
		 SELECT gen_random_uuid()::text, (?::uuid[])[1 + e.h % 6], e.id, 'whatsapp', '` + ws + `', now() FROM ` + mineEntries + ` e`, []interface{}{pq.Array(f.stages)}},
		{`INSERT INTO entry_labels (id, label_id, entry_id, entry_type, workspace_id, created_at)
		 SELECT gen_random_uuid()::text, (?::uuid[])[1 + (e.h / 3) % 8], e.id, 'whatsapp', '` + ws + `', now() FROM ` + mineEntries + ` e WHERE e.h % 3 = 0`, []interface{}{pq.Array(f.labels)}},
		{`INSERT INTO entry_stages (id, stage_id, entry_id, entry_type, workspace_id, created_at)
		 SELECT gen_random_uuid()::text, ('00000000-0000-0000-0001-' || lpad((e.h % 50)::text, 12, '0'))::uuid, e.id, 'whatsapp', e.workspace_id, now() FROM ` + noiseEntries + ` e WHERE e.h % 3 = 0`, nil},
		{`INSERT INTO entry_labels (id, label_id, entry_id, entry_type, workspace_id, created_at)
		 SELECT gen_random_uuid()::text, ('00000000-0000-0000-0002-' || lpad((e.h % 80)::text, 12, '0'))::uuid, e.id, 'whatsapp', e.workspace_id, now() FROM ` + noiseEntries + ` e WHERE e.h % 6 = 0`, nil},
		{`INSERT INTO lead_memories (id, workspace_id, lead_id, category, content, content_norm, actor_kind, actor_id, created_at, updated_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, (ARRAY['personal', 'preference', 'deal', 'objection', 'commitment', 'event', 'other'])[1 + (l.n + k) % 7],
		        (ARRAY['Gosta de futebol e mora perto da praça', 'Pediu orçamento de reforma', 'Tem dois filhos na escola municipal', 'Reclamou do atendimento', 'Interessado em consórcio'])[1 + (l.n + k) % 5],
		        'norm ' || l.id || ' ' || k, CASE WHEN (l.n + k) % 3 = 0 THEN 'human' ELSE 'ai' END, 'agent', now(), now()
		 FROM ` + mine + ` l CROSS JOIN LATERAL generate_series(1, CASE WHEN l.n % 12 = 0 THEN 3 WHEN l.n % 4 = 0 THEN 1 ELSE 0 END) k`, nil},
		{`INSERT INTO lead_memories (id, workspace_id, lead_id, category, content, content_norm, actor_kind, actor_id, created_at, updated_at)
		 SELECT gen_random_uuid(), l.workspace_id, l.id, (ARRAY['personal', 'preference', 'deal', 'objection'])[1 + l.h % 4],
		        (ARRAY['Gosta de futebol e mora perto da praça', 'Pediu orçamento de reforma', 'Reclamou do atendimento', 'Interessado em consórcio'])[1 + (l.h / 4) % 4],
		        'norm ' || l.id, 'ai', 'agent', now(), now()
		 FROM ` + noise + ` l WHERE l.h % 4 = 0`, nil},
	}
	for _, step := range steps {
		started := time.Now()
		if err := db.Exec(step.sql, step.args...).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, step.sql)
		}
		t.Logf("seeded in %s: %.80s", time.Since(started).Round(time.Second), strings.Join(strings.Fields(step.sql), " "))
	}
	seedPositionsOf(t, db, ws)
	seedReferenceVolume(t, db)
	volumeIndexes(t, db)
	mapBudgetIndexes(t, db)
	for _, table := range []string{"leads", "lead_addresses", "whatsapp_campaigns", "whatsapp_campaign_entries", "entry_stages", "entry_labels", "lead_memories"} {
		if err := db.Exec("VACUUM ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	areas := leadarea_repository.NewRepository(db)
	small, _ := leadarea.New(ws, f.viewer, leadarea.Draft{Name: "Paulista", Visibility: shared.VisibilityShared, Shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1000}}, time.Now())
	city, _ := leadarea.New(ws, f.viewer, leadarea.Draft{Name: "São Paulo", Visibility: shared.VisibilityShared, Shape: geo.Shape{Kind: geo.ShapePolygon, Ring: []geo.Point{
		{Lat: -23.80, Lng: -46.88}, {Lat: -23.80, Lng: -46.38}, {Lat: -23.30, Lng: -46.38}, {Lat: -23.30, Lng: -46.88},
	}}}, time.Now())
	for _, a := range []*leadarea.Area{&small, &city} {
		a.ID = uuid.NewString()
		if err := areas.Create(context.Background(), *a); err != nil {
			t.Fatal(err)
		}
	}
}

func (f mapBudgetFixture) inArea(t *testing.T, key, id string, more ...crmfilter.Predicate) crmfilter.Filter {
	t.Helper()
	preds := append([]crmfilter.Predicate{{Field: crmfilter.FieldArea, Key: key, Operator: crmfilter.OpIn, Values: []string{id}}}, more...)
	found, err := f.areas.FindLive(context.Background(), f.ws, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	bound, _, err := leadarea.Bind(crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: preds}}}, f.ws, f.viewer, found)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func allOf(preds ...crmfilter.Predicate) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: preds}}}
}

type mapBudgetFilter struct {
	name   string
	filter crmfilter.Filter
}

func (f mapBudgetFixture) filters(t *testing.T) []mapBudgetFilter {
	t.Helper()
	campaign := crmfilter.Predicate{Field: crmfilter.FieldCampaign, Operator: crmfilter.OpIn, Values: f.campaigns[:2]}
	read := crmfilter.Predicate{Field: crmfilter.FieldCampaignStatus, Operator: crmfilter.OpIn, Values: []string{"READ"}}
	stage := crmfilter.Predicate{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: f.stages[:1]}
	label := crmfilter.Predicate{Field: crmfilter.FieldLabel, Operator: crmfilter.OpIn, Values: f.labels[:1]}
	return []mapBudgetFilter{
		{"no filter", crmfilter.Filter{}},
		{"campaign + send status", allOf(campaign, read)},
		{"campaigns 2 to 3", allOf(crmfilter.Predicate{Field: crmfilter.FieldCampaignCount, Operator: crmfilter.OpBetween, Values: []string{"2", "3"}})},
		{"crm stage + tag", allOf(stage, label)},
		{"memory category", allOf(crmfilter.Predicate{Field: crmfilter.FieldMemoryCategory, Operator: crmfilter.OpIn, Values: []string{"objection"}})},
		{"memory author", allOf(crmfilter.Predicate{Field: crmfilter.FieldMemoryAuthor, Operator: crmfilter.OpIn, Values: []string{"human"}})},
		{"memory content", allOf(crmfilter.Predicate{Field: crmfilter.FieldMemoryText, Operator: crmfilter.OpContains, Values: []string{"orçamento"}})},
		{"memories 2 or more", allOf(crmfilter.Predicate{Field: crmfilter.FieldMemoryCount, Operator: crmfilter.OpGreaterEq, Values: []string{"2"}})},
		{"search q", allOf(crmfilter.Predicate{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"maria silva"}})},
		{"owner", allOf(crmfilter.Predicate{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIn, Values: f.owners[:1]})},
		{"area city", f.inArea(t, "", f.city)},
		{"area city exact_only", f.inArea(t, crmfilter.AreaExactOnly, f.city)},
		{"area 1 km", f.inArea(t, "", f.small)},
		{"area city + campaign status + stage", f.inArea(t, "", f.city, read, stage)},
		{"area city exact_only + memory + q", f.inArea(t, crmfilter.AreaExactOnly, f.city,
			crmfilter.Predicate{Field: crmfilter.FieldMemoryCategory, Operator: crmfilter.OpIn, Values: []string{"objection", "deal"}},
			crmfilter.Predicate{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"maria"}})},
	}
}

type mapBudgetStatement struct {
	name string
	sql  string
	args []interface{}
}

type mapBudgetSection struct {
	name   string
	budget float64
	run    func(ctx context.Context, r *mapReader, s leadmap.Scope) error
	plan   func(m mapQuery) []mapBudgetStatement
}

func layerStatements(db *gorm.DB, m mapQuery, q leadmap.LayerRequest) []mapBudgetStatement {
	positions, positionsArgs := m.layerPositions(q)
	var counted struct{ Positions int64 }
	err := inReadSession(context.Background(), db, m.settings(), func(tx *gorm.DB) error {
		return tx.Raw(positions, positionsArgs...).Scan(&counted).Error
	})
	if err != nil {
		return []mapBudgetStatement{{"positions", positions, positionsArgs}}
	}
	read, readArgs := m.layerPoints(q)
	kind := "points"
	if counted.Positions > int64(q.MaxPoints) {
		read, readArgs = m.layerCells(q)
		kind = "cells"
	}
	return []mapBudgetStatement{{"positions", positions, positionsArgs}, {kind, read, readArgs}}
}

func mapBudgetSections(t *testing.T, db *gorm.DB, spot geo.Point) []mapBudgetSection {
	t.Helper()
	centre := geo.Point{Lat: -23.55, Lng: -46.63}
	country, err := geo.SnapWindow(geo.BBox{South: -33.8, West: -74.1, North: 5.3, East: -28.8}, 4)
	if err != nil {
		t.Fatal(err)
	}
	city, err := geo.SnapWindow(geo.BBox{South: -23.67, West: -46.83, North: -23.43, East: -46.43}, 12)
	if err != nil {
		t.Fatal(err)
	}
	street, err := geo.SnapWindow(geo.BBox{South: -23.575, West: -46.675, North: -23.548, East: -46.635}, 15)
	if err != nil {
		t.Fatal(err)
	}
	layer := func(name string, budget float64, q leadmap.LayerRequest) mapBudgetSection {
		return mapBudgetSection{name, budget,
			func(ctx context.Context, r *mapReader, s leadmap.Scope) error {
				_, err := r.Layer(ctx, s, q)
				return err
			},
			func(m mapQuery) []mapBudgetStatement { return layerStatements(db, m, q) }}
	}
	sections := []mapBudgetSection{
		{"summary", mapBudgetWarmMS,
			func(ctx context.Context, r *mapReader, s leadmap.Scope) error {
				_, err := r.Summary(ctx, s)
				return err
			},
			func(m mapQuery) []mapBudgetStatement {
				sql, args := m.summary()
				return []mapBudgetStatement{{"summary", sql, args}}
			}},
		layer("layer z4 country", mapBudgetWarmMS, leadmap.NewLayerRequest(country, nil)),
		layer("layer z12 city", mapBudgetWarmMS, leadmap.NewLayerRequest(city, nil)),
		layer("layer z15 street", mapBudgetWarmMS, leadmap.NewLayerRequest(street, nil)),
	}
	for _, z := range []int{4, 12, 15} {
		tile := geo.TileOf(centre, z)
		sections = append(sections, layer(fmt.Sprintf("tile z%d %s", z, tile.Key()), mapBudgetTileWarmMS, leadmap.NewTileRequest(tile, nil)))
	}
	return append(sections,
		mapBudgetSection{"districts", mapBudgetWarmMS,
			func(ctx context.Context, r *mapReader, s leadmap.Scope) error {
				_, err := r.Districts(ctx, s, leadmap.MaxDistricts)
				return err
			},
			func(m mapQuery) []mapBudgetStatement {
				sql, args := m.districts(leadmap.MaxDistricts)
				return []mapBudgetStatement{{"districts", sql, args}}
			}},
		mapBudgetSection{"viewport", mapBudgetWarmMS, viewportReads,
			func(m mapQuery) []mapBudgetStatement {
				summary, summaryArgs := m.summary()
				extent, extentArgs := m.extent()
				return []mapBudgetStatement{{"summary", summary, summaryArgs}, {"extent", extent, extentArgs}}
			}},
		mapBudgetSection{"point", mapBudgetWarmMS,
			func(ctx context.Context, r *mapReader, s leadmap.Scope) error {
				_, _, err := r.LeadsAt(ctx, s, spot, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
				return err
			},
			func(m mapQuery) []mapBudgetStatement {
				sql, args, err := m.leadsAt(spot, crmfilter.PlacementOnMap, leadmap.MaxPeekLeads)
				if err != nil {
					t.Fatal(err)
				}
				return []mapBudgetStatement{{"point", sql, args}}
			}},
	)
}

func viewportReads(ctx context.Context, r *mapReader, s leadmap.Scope) error {
	summary, err := r.Summary(ctx, s)
	if err != nil {
		return err
	}
	if _, ok := leadmap.AreaViewport(s.Filter.BoundAreas(), summary); ok {
		return nil
	}
	extent, err := r.Extent(ctx, s)
	if err != nil || extent != nil {
		return err
	}
	_, err = r.TopCity(ctx, s)
	return err
}

type mapBudgetTiming struct {
	cold, median, p95 float64
	failure           error
}

func percentile(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	return sorted[rank]
}

func timedCall(call func() error) (float64, error) {
	started := time.Now()
	err := call()
	return float64(time.Since(started).Microseconds()) / 1000, err
}

func timeMapSection(call func() error) mapBudgetTiming {
	cold, err := timedCall(call)
	if err != nil {
		return mapBudgetTiming{cold: cold, failure: err}
	}
	warm := make([]float64, mapBudgetRuns)
	for i := range warm {
		if warm[i], err = timedCall(call); err != nil {
			return mapBudgetTiming{cold: cold, failure: err}
		}
	}
	sort.Float64s(warm)
	return mapBudgetTiming{cold: cold, median: percentile(warm, 0.5), p95: percentile(warm, 0.95)}
}

func explainOrFailure(db *gorm.DB, m mapQuery, st mapBudgetStatement) string {
	var lines []string
	err := inReadSession(context.Background(), db, m.settings(), func(tx *gorm.DB) error {
		return tx.Raw("EXPLAIN (ANALYZE, BUFFERS) "+st.sql, st.args...).Scan(&lines).Error
	})
	if err != nil {
		return "explain failed: " + err.Error()
	}
	return strings.Join(lines, "\n")
}

func scaledMS(ms float64) float64 {
	return float64(volumeBudget(time.Duration(ms*1000)*time.Microsecond).Microseconds()) / 1000
}

func TestMapSectionsWithTheHeavyListFiltersStayInsideTheirBudgetOn188kLeadsOf17MAgainstPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("generates 1.7M leads")
	}
	f := openMapBudget(t)
	reader := &mapReader{db: f.db, leads: &repository{db: f.db, agg: newAggregateCache(nil)}}
	ctx := context.Background()
	var spot geo.Point
	if err := f.db.Raw(`SELECT latitude AS lat, longitude AS lng FROM lead_addresses WHERE workspace_id = ? AND geo_precision = 'street' AND latitude BETWEEN -23.575 AND -23.548 AND longitude BETWEEN -46.675 AND -46.635 ORDER BY latitude, longitude LIMIT 1`, f.ws).Scan(&spot).Error; err != nil || spot.Lat == 0 {
		t.Fatalf("no street position near Paulista: %v", err)
	}
	only, explainOver := os.Getenv("VOZKO_MAP_BUDGET_ONLY"), os.Getenv("VOZKO_MAP_BUDGET_PLANS") == "1"
	sections := mapBudgetSections(t, f.db, spot)
	var over, rows []string
	for _, bf := range f.filters(t) {
		if only != "" && !strings.Contains(bf.name, only) {
			continue
		}
		scope := leadmap.Scope{WorkspaceID: f.ws, Filter: bf.filter}
		total, err := reader.Summary(ctx, scope)
		if err != nil {
			t.Logf("%s: the summary that counts the leads failed: %v", bf.name, err)
			total.Total = -1
		}
		if total.Total == 0 {
			t.Fatalf("%s matches no lead, so its timings say nothing", bf.name)
		}
		m, err := reader.scope(scope)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sections {
			timing := timeMapSection(func() error { return s.run(ctx, reader, scope) })
			verdict := "ok"
			if timing.failure != nil || timing.p95 > scaledMS(s.budget) || timing.cold > scaledMS(mapBudgetColdMS) {
				verdict = "OVER"
				over = append(over, bf.name+" / "+s.name)
				if explainOver {
					for _, st := range s.plan(m) {
						t.Logf("plan of %s / %s / %s:\n%s", bf.name, s.name, st.name, explainOrFailure(f.db, m, st))
					}
				}
			}
			row := fmt.Sprintf("| %s | %d | %s | %.1f | %.1f | %.1f | %s (%.0f) |", bf.name, total.Total, s.name, timing.cold, timing.median, timing.p95, verdict, s.budget)
			if timing.failure != nil {
				row = fmt.Sprintf("| %s | %d | %s | %.1f | failed: %v | | OVER (%.0f) |", bf.name, total.Total, s.name, timing.cold, timing.failure, s.budget)
			}
			t.Log(row)
			rows = append(rows, row)
		}
	}
	t.Log("\n| filter | leads | section | cold ms | warm median ms | warm p95 ms | verdict (budget ms) |\n|---|---|---|---|---|---|---|\n" + strings.Join(rows, "\n"))
	if len(over) > 0 {
		t.Errorf("over the budget (warm p95 above %.0f ms, %.0f ms for tiles, or cold above %.0f ms): %s", mapBudgetWarmMS, mapBudgetTileWarmMS, mapBudgetColdMS, strings.Join(over, "; "))
	}
}
