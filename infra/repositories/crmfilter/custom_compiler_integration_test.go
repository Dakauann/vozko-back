package crmfilter

import (
	"reflect"
	"sort"
	"testing"

	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/infra/repositories/repotest"
)

func customFieldRows(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "cfcompile")
	if err := db.Exec(`CREATE TABLE cf_rows (id text PRIMARY KEY, custom_fields jsonb)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	rows := map[string]any{
		"r1": `{"nota":"Acme Corp","segmento":"smb","tags":["x","z"],"score":12.5,"visita":"2026-03-10","vip":true}`,
		"r2": `{"nota":"","segmento":"enterprise","tags":[],"score":"abc","visita":"not a date","vip":false}`,
		"r3": `{"score":"7","visita":"2026-01-05T10:00:00Z"}`,
		"r4": nil,
		"r5": `{"tags":"x"}`,
	}
	for id, doc := range rows {
		if err := db.Exec(`INSERT INTO cf_rows (id, custom_fields) VALUES (?, ?::jsonb)`, id, doc).Error; err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	return db.Session(&gorm.Session{PrepareStmt: true})
}

func TestCompileJSONBCustom_RunsOnPostgres(t *testing.T) {
	db := customFieldRows(t)

	cases := []struct {
		name string
		pred crmfilter.Predicate
		want []string
	}{
		{"text eq", boundCustom(crmfilter.KindString, "nota", crmfilter.OpEquals, "Acme Corp"), []string{"r1"}},
		{"text neq keeps rows without the key", boundCustom(crmfilter.KindString, "nota", crmfilter.OpNotEquals, "Acme Corp"), []string{"r2", "r3", "r4", "r5"}},
		{"text contains", boundCustom(crmfilter.KindString, "nota", crmfilter.OpContains, "acme"), []string{"r1"}},
		{"select in", boundCustom(crmfilter.KindEnum, "segmento", crmfilter.OpIn, "smb", "enterprise"), []string{"r1", "r2"}},
		{"select not in", boundCustom(crmfilter.KindEnum, "segmento", crmfilter.OpNotIn, "smb"), []string{"r2", "r3", "r4", "r5"}},
		{"multiselect eq", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpEquals, "x"), []string{"r1"}},
		{"multiselect in", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpIn, "x", "y"), []string{"r1"}},
		{"multiselect neq", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpNotEquals, "x"), []string{"r2", "r3", "r4", "r5"}},
		{"number gte skips text that is not a number", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpGreaterEq, "10"), []string{"r1"}},
		{"number between reads numbers stored as text", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpBetween, "5", "10"), []string{"r3"}},
		{"number eq", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpEquals, "7"), []string{"r3"}},
		{"number neq", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpNotEquals, "7"), []string{"r1", "r2", "r4", "r5"}},
		{"date before reads timestamps", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpBefore, "2026-02-01"), []string{"r3"}},
		{"date after skips text that is not a date", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpAfter, "2026-03-01"), []string{"r1"}},
		{"date between", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpBetween, "2026-01-01", "2026-12-31"), []string{"r1", "r3"}},
		{"boolean true", boundCustom(crmfilter.KindBool, "vip", crmfilter.OpIsTrue), []string{"r1"}},
		{"boolean false", boundCustom(crmfilter.KindBool, "vip", crmfilter.OpIsFalse), []string{"r2"}},
		{"is set ignores empty arrays", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpIsSet), []string{"r1", "r5"}},
		{"is empty counts blank text and missing keys", boundCustom(crmfilter.KindString, "nota", crmfilter.OpIsEmpty), []string{"r2", "r3", "r4", "r5"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			where, args, err := Compile(crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, tc.pred)}}, NewOpportunityDescriptor(), 1)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			var ids []string
			if err := db.Table("cf_rows o").Where(where, args...).Pluck("o.id", &ids).Error; err != nil {
				t.Fatalf("query %s: %v", where, err)
			}
			sort.Strings(ids)
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("ids = %v, want %v (%s)", ids, tc.want, where)
			}
		})
	}
}
