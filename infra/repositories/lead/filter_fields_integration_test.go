package lead

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/infra/database/schema"
)

func TestEveryLeadFilterFieldAnswersOnPostgres(t *testing.T) {
	db := leadStoreDB(t)
	if err := db.AutoMigrate(&schema.LeadMemory{}, &schema.LeadMessageWindow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ws, owner := uuid.NewString(), uuid.NewString()
	ana, bia, caio := uuid.NewString(), uuid.NewString(), uuid.NewString()
	setup := []string{
		`INSERT INTO leads (id, workspace_id, number, name, email, nickname, source, version, birth_date, owner_id, owner_kind, opted_out_at, whatsapp_opt_in_at, relatives_count, referred_count, custom_fields, created_at, updated_at) VALUES
		 ('` + ana + `', '` + ws + `', '5511987654321', 'Ana', 'ana@gmail.com', 'Aninha', 'manual', 1, '1990-10-08', '` + owner + `', 'human', NULL, now(), 1, 1, '{"classificacao": "positivo"}', now(), now()),
		 ('` + bia + `', '` + ws + `', NULL, 'Bia', NULL, NULL, 'import', 1, '1985-12-31', NULL, NULL, now(), NULL, 1, 0, NULL, now(), now()),
		 ('` + caio + `', '` + ws + `', '5531988887777', 'Caio', NULL, NULL, 'channel', 1, NULL, NULL, NULL, NULL, NULL, 0, 0, '{"classificacao": "negativo"}', now(), now())`,
		`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label, position, created_at) VALUES
		 (gen_random_uuid(), '` + ws + `', '` + bia + `', '5511987654321', 'landline', 0, now())`,
		`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, zip_code, district, district_key, city, city_key, state, geo_status, geo_precision, fingerprint, created_at, updated_at) VALUES
		 (gen_random_uuid(), '` + ws + `', '` + ana + `', 'home', true, 0, '01310100', 'Centro', 'centro', 'São Paulo', 'sp:sao paulo', 'SP', 'located', 'address', 'f1', now(), now()),
		 (gen_random_uuid(), '` + ws + `', '` + caio + `', 'home', true, 0, '32010000', 'Centro', 'centro', 'Contagem', 'mg:contagem', 'MG', 'pending', NULL, 'f2', now(), now())`,
		`INSERT INTO lead_relations (id, workspace_id, lead_id, other_lead_id, dimension, kind, created_at) VALUES
		 (gen_random_uuid(), '` + ws + `', '` + ana + `', '` + bia + `', 'family', 'child', now()),
		 (gen_random_uuid(), '` + ws + `', '` + ana + `', '` + caio + `', 'referral', 'referred', now())`,
	}
	for _, sql := range setup {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	one := func(field crmfilter.Field, op crmfilter.Operator, values ...string) crmfilter.Filter {
		return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: field, Operator: op, Values: values}}}}}
	}
	cases := []struct {
		name   string
		filter crmfilter.Filter
		want   int64
	}{
		{"id", one(crmfilter.FieldID, crmfilter.OpIn, ana, caio), 2},
		{"a phone held as identity or contact", one(crmfilter.FieldPhoneAny, crmfilter.OpIn, "551187654321"), 2},
		{"no such phone", one(crmfilter.FieldPhoneAny, crmfilter.OpNotIn, "5511987654321"), 1},
		{"number search reads contact phones", one(crmfilter.FieldNumber, crmfilter.OpContains, "98765"), 2},
		{"query reads contact phones", one(crmfilter.FieldQuery, crmfilter.OpContains, "87654321"), 2},
		{"email", one(crmfilter.FieldEmail, crmfilter.OpContains, "GMAIL"), 1},
		{"nickname", one(crmfilter.FieldNickname, crmfilter.OpIsSet), 1},
		{"birthday today", one(crmfilter.FieldBirthday, crmfilter.OpEquals, crmfilter.BirthdayToday), 1},
		{"birth date until the last day of 1985", one(crmfilter.FieldBirthDate, crmfilter.OpLessEq, "1985-12-31"), 1},
		{"owner", one(crmfilter.FieldOwner, crmfilter.OpIn, owner), 1},
		{"without owner", one(crmfilter.FieldOwner, crmfilter.OpIsEmpty), 2},
		{"source", one(crmfilter.FieldSource, crmfilter.OpIn, "import", "channel"), 2},
		{"zip", one(crmfilter.FieldZip, crmfilter.OpEquals, "01310-100"), 1},
		{"state", one(crmfilter.FieldState, crmfilter.OpIn, "mg"), 1},
		{"city", one(crmfilter.FieldCity, crmfilter.OpIn, "sp:sao paulo"), 1},
		{"a bairro is a pair, never a bare name", one(crmfilter.FieldDistrict, crmfilter.OpIn, "mg:contagem/centro"), 1},
		{"precision", one(crmfilter.FieldGeoPrecision, crmfilter.OpIn, "address"), 1},
		{"geo status", one(crmfilter.FieldGeoStatus, crmfilter.OpIn, "pending"), 1},
		{"without address", one(crmfilter.FieldHasAddress, crmfilter.OpIsFalse), 1},
		{"without identity", one(crmfilter.FieldHasIdentity, crmfilter.OpIsFalse), 1},
		{"opted out", one(crmfilter.FieldOptedOut, crmfilter.OpIsTrue), 1},
		{"opted in", one(crmfilter.FieldWhatsAppOptIn, crmfilter.OpIsTrue), 1},
		{"has a parent", one(crmfilter.FieldRelationKind, crmfilter.OpIn, "parent"), 1},
		{"has a child", one(crmfilter.FieldRelationKind, crmfilter.OpIn, "child"), 1},
		{"has any relation", one(crmfilter.FieldRelationKind, crmfilter.OpIsSet), 3},
		{"relatives count", one(crmfilter.FieldRelativesCount, crmfilter.OpGreaterEq, "1"), 2},
		{"referred count", one(crmfilter.FieldReferredCount, crmfilter.OpEquals, "1"), 1},
		{"referred by", one(crmfilter.FieldReferredBy, crmfilter.OpIn, ana), 1},
		{"custom select", crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpIn, Values: []string{"positivo"}}.BindKind(crmfilter.KindEnum),
		}}}}, 1},
	}
	today := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: tc.filter, Today: today})
			if err != nil {
				t.Fatalf("ListWithSummary() error = %v", err)
			}
			if page.TotalItems != tc.want || int64(len(page.Items)) != tc.want {
				t.Fatalf("total = %d, items = %d, want %d", page.TotalItems, len(page.Items), tc.want)
			}
		})
	}
}
