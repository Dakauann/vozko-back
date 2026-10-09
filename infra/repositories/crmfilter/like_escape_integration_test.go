package crmfilter

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/infra/database"
	"vozko/infra/repositories/repotest"
)

func TestContainsTreatsWildcardsAsTextOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "cflike")
	if err := db.Exec(`CREATE TABLE cf_rows (id text PRIMARY KEY, title text, custom_fields jsonb)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	for id, text := range map[string]string{"r1": "50% off", "r2": "500 off", "r3": "lead_1", "r4": "leadX1", `r5`: `pasta\docs`} {
		if err := db.Exec(`INSERT INTO cf_rows (id, title, custom_fields) VALUES (?, ?, jsonb_build_object('nota', ?::text))`, id, text, text).Error; err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	db = db.Session(&gorm.Session{PrepareStmt: true})

	for _, tc := range []struct {
		typed string
		want  []string
	}{
		{"50%", []string{"r1"}},
		{"d_1", []string{"r3"}},
		{`a\d`, []string{"r5"}},
	} {
		for name, p := range map[string]crmfilter.Predicate{
			"title":  pred(crmfilter.FieldQuery, crmfilter.OpContains, tc.typed),
			"custom": boundCustom(crmfilter.KindString, "nota", crmfilter.OpContains, tc.typed),
		} {
			t.Run(name+" "+tc.typed, func(t *testing.T) {
				var where string
				var args []interface{}
				var err error
				if name == "title" {
					where, args, err = compileColumn(FieldMapping{Expr: "o.title", Kind: crmfilter.KindText}, p)
				} else {
					where, args, err = compileJSONBCustom(p, "o.custom_fields")
				}
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				var ids []string
				if err := db.Table("cf_rows o").Where(where, args...).Pluck("o.id", &ids).Error; err != nil {
					t.Fatalf("query %s: %v", where, err)
				}
				sort.Strings(ids)
				if !reflect.DeepEqual(ids, tc.want) {
					t.Fatalf("ids = %v, want %v", ids, tc.want)
				}
			})
		}
	}
}

func TestLeadSearchTreatsWildcardsAsTextOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "leadlike")
	for _, ddl := range []string{
		`CREATE TABLE leads (id text PRIMARY KEY, workspace_id text, name text, nickname text, number text, deleted_at timestamptz)`,
		`CREATE TABLE lead_phones (lead_id text, workspace_id text, number text)`,
		`CREATE TABLE lead_addresses (lead_id text, workspace_id text, is_primary boolean, district_key text, city_key text)`,
		`CREATE TABLE lead_memories (lead_id text, workspace_id text, deleted_at timestamptz, content text)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if err := database.CreateSearchFold(db); err != nil {
		t.Fatalf("search fold: %v", err)
	}
	for _, row := range [][]string{
		{"l1", "Promo 50% off", "5511900000001"},
		{"l2", "Promo 500 off", "5511900000002"},
		{"l3", "lead_1", "5511900000003"},
		{"l4", "leadX1", "5511900000004"},
		{"l5", `pasta\docs`, "5511900000005"},
	} {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, name, number) VALUES (?, 'ws-1', ?, ?)`, row[0], row[1], row[2]).Error; err != nil {
			t.Fatalf("insert %s: %v", row[0], err)
		}
	}
	db = db.Session(&gorm.Session{PrepareStmt: true})
	desc := LeadDescriptor{Alias: "leads", WorkspaceID: "ws-1"}

	for _, tc := range []struct {
		field crmfilter.Field
		typed string
		want  []string
	}{
		{crmfilter.FieldQuery, "50%", []string{"l1"}},
		{crmfilter.FieldQuery, "d_1", []string{"l3"}},
		{crmfilter.FieldQuery, `a\d`, []string{"l5"}},
		{crmfilter.FieldNumber, "_", []string{}},
		{crmfilter.FieldNumber, "%", []string{}},
		{crmfilter.FieldNumber, "0003", []string{"l3"}},
	} {
		t.Run(string(tc.field)+" "+tc.typed, func(t *testing.T) {
			p := pred(tc.field, crmfilter.OpContains, tc.typed)
			var where string
			var args []interface{}
			var err error
			if tc.field == crmfilter.FieldQuery {
				where, args, err = desc.compileQuery(p)
			} else {
				where, args, err = desc.compileNumber(p)
			}
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			ids := []string{}
			if err := db.Table("leads").Where(where, args...).Pluck("leads.id", &ids).Error; err != nil {
				t.Fatalf("query %s: %v", where, err)
			}
			sort.Strings(ids)
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("ids = %v, want %v", ids, tc.want)
			}
		})
	}
	if _, _, err := desc.compileQuery(pred(crmfilter.FieldQuery, crmfilter.OpContains, `\`)); !errors.Is(err, lead.ErrLeadSearchTooShort) {
		t.Fatalf("a lone backslash is not a word: %v", err)
	}
}
