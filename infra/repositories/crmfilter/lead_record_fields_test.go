package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
)

const (
	testWorkspace  = "8f2d6a4e-1c3b-4d5e-9f7a-0b1c2d3e4f5a"
	someLead       = "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"
	primaryAddress = "leads.id IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary"
	contactPhones  = "SELECT lp_f.lead_id FROM lead_phones lp_f WHERE lp_f.workspace_id = ? AND lp_f.number"
	monthDay       = "(extract(month from leads.birth_date), extract(day from leads.birth_date))"
	ownerPairs     = "(leads.owner_id, leads.owner_kind) IN (SELECT * FROM unnest(?::uuid[], ?::text[]))"
)

func scopedLeadDesc() LeadDescriptor {
	return LeadDescriptor{Alias: "leads", WorkspaceID: testWorkspace, Today: time.Date(2026, time.December, 30, 10, 0, 0, 0, time.UTC)}
}

func TestLeadRecordFields_GoldenSQL(t *testing.T) {
	cases := []struct {
		name     string
		pred     crmfilter.Predicate
		wantSQL  string
		wantArgs []interface{}
	}{
		{"id in", pred(crmfilter.FieldID, crmfilter.OpIn, someLead),
			"leads.id = ANY(?::uuid[])", []interface{}{pq.Array([]string{someLead})}},
		{"id not in", pred(crmfilter.FieldID, crmfilter.OpNotIn, someLead),
			"leads.id <> ALL(?::uuid[])", []interface{}{pq.Array([]string{someLead})}},
		{"any phone in both ninth digit formats", pred(crmfilter.FieldPhoneAny, crmfilter.OpIn, "(11) 98765-4321"),
			"(leads.number = ANY(?) OR leads.id IN (" + contactPhones + " = ANY(?)))",
			[]interface{}{pq.Array([]string{"5511987654321", "551187654321"}), testWorkspace, pq.Array([]string{"5511987654321", "551187654321"})}},
		{"any phone excluded", pred(crmfilter.FieldPhoneAny, crmfilter.OpNotIn, "5511987654321"),
			"NOT COALESCE((leads.number = ANY(?) OR leads.id IN (" + contactPhones + " = ANY(?))), false)",
			[]interface{}{pq.Array([]string{"5511987654321", "551187654321"}), testWorkspace, pq.Array([]string{"5511987654321", "551187654321"})}},
		{"number contains also reads contact phones", pred(crmfilter.FieldNumber, crmfilter.OpContains, "8765"),
			"(leads.number LIKE ? OR leads.id IN (" + contactPhones + " LIKE ?))",
			[]interface{}{"%8765%", testWorkspace, "%8765%"}},
		{"number presence is the identity", pred(crmfilter.FieldNumber, crmfilter.OpIsSet),
			"NULLIF(leads.number, '') IS NOT NULL", nil},
		{"email", pred(crmfilter.FieldEmail, crmfilter.OpContains, "gmail"),
			"leads.email ILIKE ?", []interface{}{"%gmail%"}},
		{"nickname", pred(crmfilter.FieldNickname, crmfilter.OpIsSet),
			"leads.nickname IS NOT NULL", nil},
		{"birthday this week across the year end", pred(crmfilter.FieldBirthday, crmfilter.OpEquals, crmfilter.BirthdayThisWeek),
			"leads.birth_date IS NOT NULL AND ((" + monthDay + " >= (?, ?) AND " + monthDay + " <= (?, ?)) OR (" + monthDay + " >= (?, ?) AND " + monthDay + " <= (?, ?)))",
			[]interface{}{12, 27, 12, 31, 1, 1, 1, 2}},
		{"birth date", pred(crmfilter.FieldBirthDate, crmfilter.OpBefore, "1990-01-01"),
			"leads.birth_date < ?::date", []interface{}{"1990-01-01"}},
		{"owner among actors", pred(crmfilter.FieldOwner, crmfilter.OpIn, someLead, "ai:"+someLead),
			ownerPairs, []interface{}{pq.Array([]string{someLead, someLead}), pq.Array([]string{"human", "ai"})}},
		{"owner excluded", pred(crmfilter.FieldOwner, crmfilter.OpNotIn, "workflow:"+someLead),
			"NOT COALESCE(" + ownerPairs + ", false)", []interface{}{pq.Array([]string{someLead}), pq.Array([]string{"workflow"})}},
		{"without owner", pred(crmfilter.FieldOwner, crmfilter.OpIsEmpty),
			"leads.owner_id IS NULL", nil},
		{"source", pred(crmfilter.FieldSource, crmfilter.OpIn, "import"),
			"leads.source = ANY(?)", []interface{}{pq.Array([]string{"import"})}},
		{"zip on the primary address", pred(crmfilter.FieldZip, crmfilter.OpIn, "01310-100"),
			primaryAddress + " AND la_f.zip_code = ANY(?))", []interface{}{testWorkspace, pq.Array([]string{"01310100"})}},
		{"state", pred(crmfilter.FieldState, crmfilter.OpNotIn, "sp"),
			"leads.id NOT IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary AND la_f.state = ANY(?))",
			[]interface{}{testWorkspace, pq.Array([]string{"SP"})}},
		{"city keys as stored", pred(crmfilter.FieldCity, crmfilter.OpIn, "SP:São Paulo"),
			primaryAddress + " AND la_f.city_key = ANY(?))", []interface{}{testWorkspace, pq.Array([]string{"sp:sao paulo"})}},
		{"city presence", pred(crmfilter.FieldCity, crmfilter.OpIsSet),
			primaryAddress + " AND la_f.city_key IS NOT NULL)", []interface{}{testWorkspace}},
		{"district pairs as stored", pred(crmfilter.FieldDistrict, crmfilter.OpIn, "sp:sao paulo/Jd. Paulista", "MG:Contagem/centro"),
			primaryAddress + " AND (la_f.city_key, la_f.district_key) IN (SELECT * FROM unnest(?::text[], ?::text[])))",
			[]interface{}{testWorkspace, pq.Array([]string{"sp:sao paulo", "mg:contagem"}), pq.Array([]string{"jardim paulista", "centro"})}},
		{"geo precision", pred(crmfilter.FieldGeoPrecision, crmfilter.OpIn, "address"),
			primaryAddress + " AND la_f.geo_precision = ANY(?))", []interface{}{testWorkspace, pq.Array([]string{"address"})}},
		{"geo status", pred(crmfilter.FieldGeoStatus, crmfilter.OpEquals, "pending"),
			primaryAddress + " AND la_f.geo_status = ANY(?))", []interface{}{testWorkspace, pq.Array([]string{"pending"})}},
		{"with an address", pred(crmfilter.FieldHasAddress, crmfilter.OpIsTrue),
			primaryAddress + ")", []interface{}{testWorkspace}},
		{"without an address", pred(crmfilter.FieldHasAddress, crmfilter.OpIsFalse),
			"leads.id NOT IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary)", []interface{}{testWorkspace}},
		{"has identity", pred(crmfilter.FieldHasIdentity, crmfilter.OpIsFalse),
			"NULLIF(leads.number, '') IS NULL", nil},
		{"opted out", pred(crmfilter.FieldOptedOut, crmfilter.OpIsTrue),
			"leads.opted_out_at IS NOT NULL", nil},
		{"whatsapp opt in", pred(crmfilter.FieldWhatsAppOptIn, crmfilter.OpIsFalse),
			"leads.whatsapp_opt_in_at IS NULL", nil},
		{"relation kind from either side", pred(crmfilter.FieldRelationKind, crmfilter.OpIn, "parent"),
			"leads.id IN (SELECT lr_k.lead_id FROM lead_relations lr_k WHERE lr_k.workspace_id = ? AND lr_k.kind = ANY(?) UNION ALL SELECT lr_i.other_lead_id FROM lead_relations lr_i WHERE lr_i.workspace_id = ? AND lr_i.kind = ANY(?))",
			[]interface{}{testWorkspace, pq.Array([]string{"parent"}), testWorkspace, pq.Array([]string{"child"})}},
		{"any relation", pred(crmfilter.FieldRelationKind, crmfilter.OpIsEmpty),
			"leads.id NOT IN (SELECT lr_k.lead_id FROM lead_relations lr_k WHERE lr_k.workspace_id = ? UNION ALL SELECT lr_i.other_lead_id FROM lead_relations lr_i WHERE lr_i.workspace_id = ?)",
			[]interface{}{testWorkspace, testWorkspace}},
		{"relatives count", pred(crmfilter.FieldRelativesCount, crmfilter.OpGreaterEq, "2"),
			"leads.relatives_count >= ?", []interface{}{float64(2)}},
		{"referred count", pred(crmfilter.FieldReferredCount, crmfilter.OpBetween, "1", "5"),
			"leads.referred_count BETWEEN ? AND ?", []interface{}{float64(1), float64(5)}},
		{"referred by", pred(crmfilter.FieldReferredBy, crmfilter.OpIn, someLead),
			"leads.id IN (SELECT lr_b.other_lead_id FROM lead_relations lr_b WHERE lr_b.workspace_id = ? AND lr_b.kind = ? AND lr_b.lead_id = ANY(?::uuid[]))",
			[]interface{}{testWorkspace, "referred", pq.Array([]string{someLead})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), tc.pred)
			if want := "(" + tc.wantSQL + ")"; sql != want {
				t.Fatalf("SQL\n got: %s\nwant: %s", sql, want)
			}
			if len(args) == 0 && len(tc.wantArgs) == 0 {
				return
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
			if got := strings.Count(sql, "?"); got != len(args) {
				t.Fatalf("%d placeholders for %d args in %s", got, len(args), sql)
			}
		})
	}
}

func TestLeadBirthdayTodayMatchesTheIndexedExpression(t *testing.T) {
	desc := scopedLeadDesc()
	desc.Today = time.Date(2026, time.October, 8, 23, 30, 0, 0, time.UTC)
	sql, args := compileLead(t, desc, pred(crmfilter.FieldBirthday, crmfilter.OpEquals, crmfilter.BirthdayToday))
	if strings.Contains(sql, "date_part") || strings.Count(sql, "extract(month from leads.birth_date)") != 2 {
		t.Fatalf("birthday must repeat the indexed extract(...) expression, got %s", sql)
	}
	if !reflect.DeepEqual(args, []interface{}{10, 8, 10, 8}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestDistrictPairsBindTwoArraysWhateverTheirCount(t *testing.T) {
	pairs := make([]string, crmfilter.MaxDistrictPairs)
	for i := range pairs {
		pairs[i] = "sp:sao paulo/bairro " + string(rune(0x61+i%26)) + strings.Repeat("x", i/26)
	}
	preds := make([]crmfilter.Predicate, 0, 100)
	for range 100 {
		preds = append(preds, pred(crmfilter.FieldDistrict, crmfilter.OpIn, pairs...))
	}
	sql, args := compileLead(t, scopedLeadDesc(), preds...)
	if got := strings.Count(sql, "?"); got != 300 || len(args) != 300 {
		t.Fatalf("100 predicates of %d pairs bind %d placeholders and %d args, want 300", crmfilter.MaxDistrictPairs, got, len(args))
	}
}

func TestLeadRecordFields_RefuseWhatCannotBeScoped(t *testing.T) {
	unscoped := NewLeadDescriptor()
	for _, p := range []crmfilter.Predicate{
		pred(crmfilter.FieldPhoneAny, crmfilter.OpIn, "5511987654321"),
		pred(crmfilter.FieldZip, crmfilter.OpIn, "01310100"),
		pred(crmfilter.FieldDistrict, crmfilter.OpIn, "sp:sao paulo/centro"),
		pred(crmfilter.FieldHasAddress, crmfilter.OpIsTrue),
		pred(crmfilter.FieldRelationKind, crmfilter.OpIn, "parent"),
		pred(crmfilter.FieldReferredBy, crmfilter.OpIn, someLead),
		pred(crmfilter.FieldQuery, crmfilter.OpContains, "ana"),
	} {
		f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{p}}}}
		if _, _, err := Compile(f, unscoped, 0); !errors.Is(err, crmfilter.ErrNotApplicable) {
			t.Fatalf("%s without a workspace = %v, want ErrNotApplicable", p.Field, err)
		}
	}

	withoutDay := scopedLeadDesc()
	withoutDay.Today = time.Time{}
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		pred(crmfilter.FieldBirthday, crmfilter.OpEquals, crmfilter.BirthdayToday),
	}}}}
	if _, _, err := Compile(f, withoutDay, 0); !errors.Is(err, crmfilter.ErrBirthdayClockMissing) {
		t.Fatalf("birthday without the workspace day = %v, want ErrBirthdayClockMissing", err)
	}
}

func TestLeadCustomFieldsCompileThroughTheBoundKind(t *testing.T) {
	bound := crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"positivo"}}.BindKind(crmfilter.KindEnum)
	sql, args := compileLead(t, scopedLeadDesc(), bound)
	if want := "(leads.custom_fields @> jsonb_build_object(?::text, ?::text))"; sql != want {
		t.Fatalf("custom SQL = %s, want %s", sql, want)
	}
	if !reflect.DeepEqual(args, []interface{}{"classificacao", "positivo"}) {
		t.Fatalf("args = %#v", args)
	}

	unbound := crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"positivo"}}
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{unbound}}}}
	if _, _, err := Compile(f, scopedLeadDesc(), 0); !errors.Is(err, ErrUnboundCustomField) {
		t.Fatalf("an unbound custom predicate = %v, want ErrUnboundCustomField", err)
	}
}
