package lead

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database"
)

const (
	searchVolumeLeads = 188000
	searchNoiseLeads  = 1512000
)

const searchFirstNames = `ARRAY['Maria','José','João','Antônio','Ana','Francisco','Carlos','Paulo','Pedro','Lucas','Luiz','Marcos','Luís','Gabriel','Rafael','Daniel','Marcelo','Bruno','Eduardo','Felipe','Raimundo','Rodrigo','Manoel','Mateus','André','Fernando','Fábio','Leonardo','Gustavo','Guilherme','Juliana','Adriana','Márcia','Fernanda','Patrícia','Aline','Sandra','Camila','Amanda','Bruna','Jéssica','Letícia','Júlia','Luciana','Vanessa','Mariana','Gabriela','Vera','Vitória','Larissa','Cláudia','Beatriz','Luana','Rita','Sônia','Renata','Eliane','Josefa','Simone','Natália']`

const searchSurnames = `ARRAY['Silva','Santos','Oliveira','Souza','Rodrigues','Ferreira','Alves','Pereira','Lima','Gomes','Costa','Ribeiro','Martins','Carvalho','Almeida','Lopes','Soares','Fernandes','Vieira','Barbosa','Rocha','Dias','Nascimento','Andrade','Moreira','Nunes','Marques','Machado','Mendes','Freitas','Cardoso','Ramos','Gonçalves','Santana','Teixeira','Araújo','Conceição','Medeiros']`

func searchVolumeDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	db := leadStoreDB(t)
	volumeTables(t, db)
	ws := uuid.NewString()
	ranked := "(SELECT id, workspace_id, row_number() OVER (PARTITION BY workspace_id = '" + ws + "' ORDER BY id) AS n, workspace_id = '" + ws + "' AS mine FROM leads)"
	steps := []string{
		`INSERT INTO leads (id, workspace_id, number, name, nickname, source, version, created_at, updated_at)
		 SELECT gen_random_uuid(), CASE WHEN g <= ` + itoa(searchVolumeLeads) + ` THEN '` + ws + `'::uuid ELSE ('00000000-0000-0000-0000-' || lpad((g % 400)::text, 12, '0'))::uuid END,
		        '558499' || lpad(g::text, 7, '0'),
		        f[1 + (g::bigint * 7919) % 60] || ' ' || s[1 + (g::bigint * 104729) % 38] || ' ' || s[1 + (g::bigint * 15485863) % 38],
		        CASE WHEN g % 10 = 0 THEN 'Apelido ' || (g % 997) END, 'import', 1, now() - (g || ' seconds')::interval, now()
		 FROM generate_series(1, ` + itoa(searchVolumeLeads+searchNoiseLeads) + `) g, (SELECT ` + searchFirstNames + ` AS f, ` + searchSurnames + ` AS s) names`,
		`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, district, city, state, geo_status, fingerprint, created_at, updated_at)
		 SELECT gen_random_uuid(), r.workspace_id, r.id, 'home', true, 0,
		        CASE WHEN r.mine AND r.n IN (5, 50000) THEN 'Santo Antônio'
		             ELSE (ARRAY['Alecrim','Lagoa Nova','Ponta Negra','Tirol','Petrópolis','Cidade da Esperança','Nossa Senhora de Nazaré','Quintas','Rocas','Ribeira'])[1 + r.n % 10] || ' ' || (r.n % 30) END,
		        (ARRAY['Natal','Parnamirim','Mossoró','São Gonçalo do Amarante','Macaíba','Caicó'])[1 + r.n % 6], 'RN', 'pending', md5(r.id::text), now(), now()
		 FROM ` + ranked + ` r WHERE r.n % 5 < 2`,
		`UPDATE lead_addresses SET district_key = vozko_fold(district), city_key = 'rn:' || vozko_fold(city)`,
		`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label, position, created_at)
		 SELECT gen_random_uuid(), workspace_id, id, '55843' || substr(number, 6), 'landline', 0, now() FROM leads WHERE substr(number, 13, 1) = '0'`,
		`INSERT INTO lead_memories (id, workspace_id, lead_id, category, content, content_norm, actor_kind, actor_id, created_at, updated_at)
		 SELECT gen_random_uuid(), workspace_id, id, 'interest',
		        (ARRAY['Gosta de futebol e mora perto da praça','Pediu orçamento de reforma','Tem dois filhos na escola municipal','Reclamou do atendimento','Interessado em consórcio'])[1 + abs(hashtext(id::text)) % 5],
		        'norm ' || id, 'ai', 'agent', now(), now()
		 FROM leads WHERE abs(hashtext(id::text)) % 4 = 0`,
	}
	for _, sql := range steps {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	volumeIndexes(t, db)
	for _, table := range []string{"leads", "lead_addresses", "lead_phones", "lead_memories"} {
		if err := db.Exec("VACUUM ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, ws
}

func bruteForceSearchCount(t *testing.T, db *gorm.DB, ws string, words []string) int64 {
	t.Helper()
	sql := "SELECT COUNT(*) FROM leads l LEFT JOIN lead_addresses a ON a.lead_id = l.id AND a.is_primary WHERE l.workspace_id = ? AND l.deleted_at IS NULL"
	args := []interface{}{ws}
	for _, word := range words {
		sql += " AND (vozko_fold(l.name) LIKE ? OR vozko_fold(coalesce(l.nickname, '')) LIKE ? OR coalesce(a.district_key, '') LIKE ? OR coalesce(a.city_key, '') LIKE ?" +
			" OR EXISTS (SELECT 1 FROM lead_memories m WHERE m.lead_id = l.id AND m.deleted_at IS NULL AND vozko_fold(m.content) LIKE ?))"
		p := "%" + word + "%"
		args = append(args, p, p, p, p, p)
	}
	var n int64
	if err := db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func warmExplain(t *testing.T, db *gorm.DB, sql string, args ...interface{}) (time.Duration, string) {
	t.Helper()
	best, plan := explain(t, db, sql, args...)
	for i := 0; i < 2; i++ {
		took, again := explain(t, db, sql, args...)
		if took < best {
			best, plan = took, again
		}
	}
	return best, plan
}

func TestTheLeadSearchUsesTheTrigramIndexesAndStaysWithinTheListBudgetOn188kLeadsAgainstPostgres(t *testing.T) {
	db, ws := searchVolumeDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	budget := volumeBudget(volumeListBudget)

	var firstNumber string
	if err := db.Raw(`SELECT number FROM leads WHERE workspace_id = ? ORDER BY number LIMIT 1 OFFSET 777`, ws).Scan(&firstNumber).Error; err != nil {
		t.Fatal(err)
	}
	withoutNinth := firstNumber[2:4] + firstNumber[5:]

	cases := []struct {
		query   string
		words   []string
		indexes []string
		total   int64
	}{
		{"Santo Antonio", []string{"santo", "antonio"}, []string{database.LeadSearchNameIndex, database.LeadAddressPlaceLeadIndex}, -1},
		{"maria natal", []string{"maria", "natal"}, []string{database.LeadSearchNameIndex, database.LeadAddressPlaceLeadIndex}, -1},
		{"JOÃO silva", []string{"joao", "silva"}, []string{database.LeadSearchNameIndex}, -1},
		{"silva", []string{"silva"}, []string{database.LeadSearchNameIndex}, -1},
		{"natal", []string{"natal"}, []string{database.LeadAddressPlaceLeadIndex}, -1},
		{"jo", nil, []string{database.LeadSearchNameIndex}, -1},
		{"maria da silva", nil, []string{database.LeadSearchNameIndex}, -1},
		{"joao 4123", nil, []string{database.LeadSearchNumberIndex}, -1},
		{"4123", nil, []string{database.LeadSearchNumberIndex}, -1},
		{withoutNinth, nil, nil, 1},
	}
	for _, tc := range cases {
		input := lead.ListLeadsInput{WorkspaceID: ws, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{tc.query}},
		}}}}, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 50}}}
		page, err := repo.ListWithSummary(input)
		if err != nil {
			t.Fatalf("%s: %v", tc.query, err)
		}
		want := tc.total
		if tc.words != nil {
			want = bruteForceSearchCount(t, db, ws, tc.words)
		}
		if want >= 0 && page.TotalItems != want {
			t.Errorf("%q found %d leads, want %d", tc.query, page.TotalItems, want)
		}
		q, err := repo.compile(input)
		if err != nil {
			t.Fatal(err)
		}
		idSQL, idArgs := q.pageIDs(input.Options)
		idTook, plan := warmExplain(t, db, idSQL, idArgs...)
		countTook, _ := warmExplain(t, db, "SELECT COUNT(*) FROM leads WHERE "+q.where, q.args...)
		t.Logf("search %-16q total %6d ids %8s count %8s", tc.query, page.TotalItems, idTook.Round(time.Millisecond/10), countTook.Round(time.Millisecond/10))
		if idTook > budget || countTook > budget {
			t.Errorf("search %q reads its ids in %s and counts in %s, over the %s budget\n%s", tc.query, idTook, countTook, budget, plan)
		}
		for _, index := range tc.indexes {
			if !strings.Contains(plan, index) {
				t.Errorf("search %q does not use %s\n%s", tc.query, index, plan)
			}
		}
	}

	santo, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"Santo Antonio"}},
		{Field: crmfilter.FieldDistrict, Operator: crmfilter.OpIn, Values: []string{"rn:natal/santo antonio", "rn:parnamirim/santo antonio", "rn:mossoro/santo antonio", "rn:sao goncalo do amarante/santo antonio", "rn:macaiba/santo antonio", "rn:caico/santo antonio"}},
	}}}}})
	if err != nil || santo.TotalItems != 2 {
		t.Fatalf("the two leads living in Santo Antônio = %d, %v", santo.TotalItems, err)
	}

	maria, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"maria"}},
	}}}}, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}})
	if err != nil || len(maria.Items) == 0 {
		t.Fatalf("maria = %v", err)
	}
	for _, item := range maria.Items {
		if !strings.HasPrefix(shared.FoldForMatch(item.Lead.Name), "maria") {
			t.Fatalf("names starting with the search come first, got %q", item.Lead.Name)
		}
	}

	places, err := repo.ReadPlaces(context.Background(), lead.SectionQuery{WorkspaceID: ws, PlacePrefix: "santo"})
	if err != nil || len(places.Cities) != 0 || len(places.Districts) == 0 {
		t.Fatalf("place suggestions for santo = %+v, %v", places, err)
	}
	living := int64(0)
	for _, district := range places.Districts {
		if district.District != "Santo Antônio" {
			t.Fatalf("a suggestion that does not start with santo: %+v", district)
		}
		living += district.Count
	}
	if living != 2 {
		t.Fatalf("place suggestions count %d leads in Santo Antônio, want 2", living)
	}
}
