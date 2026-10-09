package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

func boundCustom(kind crmfilter.Kind, key string, op crmfilter.Operator, values ...string) crmfilter.Predicate {
	return crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: key, Operator: op, Values: values}.BindKind(kind)
}

const (
	cfCol         = "o.custom_fields"
	cfTextEq      = cfCol + " @> jsonb_build_object(?::text, ?::text)"
	cfMultiEq     = cfCol + " @> jsonb_build_object(?::text, jsonb_build_array(?::text))"
	cfBoolEq      = cfCol + " @> jsonb_build_object(?::text, ?::boolean)"
	cfAnyOf       = cfCol + " @> ANY(?::jsonb[])"
	cfTextLike    = "(" + cfCol + " ->> ?::text) ILIKE ?::text"
	cfNumber      = "(CASE WHEN pg_input_is_valid(" + cfCol + " ->> ?::text, 'numeric') THEN (" + cfCol + " ->> ?::text)::numeric END)"
	cfDate        = "(CASE WHEN pg_input_is_valid(" + cfCol + " ->> ?::text, 'date') THEN (" + cfCol + " ->> ?::text)::date END)"
	cfPresence    = "COALESCE(" + cfCol + " -> ?::text, 'null'::jsonb)"
	cfEmptyValues = "('null'::jsonb, '\"\"'::jsonb, '[]'::jsonb)"
)

func negated(sql string) string { return "NOT COALESCE(" + sql + ", false)" }

func TestCompileJSONBCustom_GoldenSQL(t *testing.T) {
	tests := []struct {
		name     string
		pred     crmfilter.Predicate
		wantSQL  string
		wantArgs []interface{}
	}{
		{"text eq", boundCustom(crmfilter.KindString, "nota", crmfilter.OpEquals, "acme"), cfTextEq, []interface{}{"nota", "acme"}},
		{"text neq", boundCustom(crmfilter.KindString, "nota", crmfilter.OpNotEquals, "acme"), negated(cfTextEq), []interface{}{"nota", "acme"}},
		{"text in", boundCustom(crmfilter.KindString, "nota", crmfilter.OpIn, "a", "b"), cfAnyOf, []interface{}{pq.Array([]string{`{"nota":"a"}`, `{"nota":"b"}`})}},
		{"text not in", boundCustom(crmfilter.KindString, "nota", crmfilter.OpNotIn, "a"), negated(cfAnyOf), []interface{}{pq.Array([]string{`{"nota":"a"}`})}},
		{"text contains", boundCustom(crmfilter.KindString, "nota", crmfilter.OpContains, "cm"), cfTextLike, []interface{}{"nota", "%cm%"}},
		{"select eq", boundCustom(crmfilter.KindEnum, "segmento", crmfilter.OpEquals, "smb"), cfTextEq, []interface{}{"segmento", "smb"}},
		{"select in", boundCustom(crmfilter.KindEnum, "segmento", crmfilter.OpIn, "smb", "enterprise"), cfAnyOf, []interface{}{pq.Array([]string{`{"segmento":"smb"}`, `{"segmento":"enterprise"}`})}},
		{"select not in", boundCustom(crmfilter.KindEnum, "segmento", crmfilter.OpNotIn, "smb"), negated(cfAnyOf), []interface{}{pq.Array([]string{`{"segmento":"smb"}`})}},
		{"multiselect eq", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpEquals, "x"), cfMultiEq, []interface{}{"tags", "x"}},
		{"multiselect neq", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpNotEquals, "x"), negated(cfMultiEq), []interface{}{"tags", "x"}},
		{"multiselect in", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpIn, "x", "y"), cfAnyOf, []interface{}{pq.Array([]string{`{"tags":["x"]}`, `{"tags":["y"]}`})}},
		{"number eq", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpEquals, "10"), cfNumber + " = ?::numeric", []interface{}{"score", "score", float64(10)}},
		{"number neq", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpNotEquals, "10"), negated(cfNumber + " = ?::numeric"), []interface{}{"score", "score", float64(10)}},
		{"number gte", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpGreaterEq, "1.5"), cfNumber + " >= ?::numeric", []interface{}{"score", "score", 1.5}},
		{"number lte", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpLessEq, "2"), cfNumber + " <= ?::numeric", []interface{}{"score", "score", float64(2)}},
		{"number between", boundCustom(crmfilter.KindNumber, "score", crmfilter.OpBetween, "1", "9"), cfNumber + " BETWEEN ?::numeric AND ?::numeric", []interface{}{"score", "score", float64(1), float64(9)}},
		{"date before", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpBefore, "2026-03-01"), cfDate + " < ?::date", []interface{}{"visita", "visita", "2026-03-01"}},
		{"date after", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpAfter, "2026-03-01T10:00:00Z"), cfDate + " > ?::date", []interface{}{"visita", "visita", "2026-03-01"}},
		{"date gte", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpGreaterEq, "2026-03-01"), cfDate + " >= ?::date", []interface{}{"visita", "visita", "2026-03-01"}},
		{"date lte", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpLessEq, "2026-03-01"), cfDate + " <= ?::date", []interface{}{"visita", "visita", "2026-03-01"}},
		{"date between", boundCustom(crmfilter.KindDate, "visita", crmfilter.OpBetween, "2026-01-01", "2026-12-31"), cfDate + " BETWEEN ?::date AND ?::date", []interface{}{"visita", "visita", "2026-01-01", "2026-12-31"}},
		{"boolean true", boundCustom(crmfilter.KindBool, "vip", crmfilter.OpIsTrue), cfBoolEq, []interface{}{"vip", true}},
		{"boolean false", boundCustom(crmfilter.KindBool, "vip", crmfilter.OpIsFalse), cfBoolEq, []interface{}{"vip", false}},
		{"boolean eq", boundCustom(crmfilter.KindBool, "vip", crmfilter.OpEquals, "false"), cfBoolEq, []interface{}{"vip", false}},
		{"is set", boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpIsSet), cfPresence + " NOT IN " + cfEmptyValues, []interface{}{"tags"}},
		{"is empty", boundCustom(crmfilter.KindString, "nota", crmfilter.OpIsEmpty), cfPresence + " IN " + cfEmptyValues, []interface{}{"nota"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, tt.pred)}}
			gotSQL, gotArgs, err := Compile(f, NewOpportunityDescriptor(), 1)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if want := "(" + tt.wantSQL + ")"; gotSQL != want {
				t.Errorf("SQL mismatch\n got: %s\nwant: %s", gotSQL, want)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args mismatch\n got: %#v\nwant: %#v", gotArgs, tt.wantArgs)
			}
			if placeholders := strings.Count(gotSQL, "?"); placeholders != len(gotArgs) {
				t.Errorf("%d placeholders for %d args: %s", placeholders, len(gotArgs), gotSQL)
			}
		})
	}
}

func TestCompileJSONBCustom_RefusesAnUnboundPredicate(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or,
		crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "score", Operator: crmfilter.OpEquals, Values: []string{"1"}},
	)}}
	if _, _, err := Compile(f, NewOpportunityDescriptor(), 1); !errors.Is(err, ErrUnboundCustomField) {
		t.Fatalf("Compile() error = %v, want ErrUnboundCustomField", err)
	}
}

func TestCompileJSONBCustom_RefusesAnOperatorItsKindCannotCompile(t *testing.T) {
	cases := []crmfilter.Predicate{
		boundCustom(crmfilter.KindNumber, "score", crmfilter.OpContains, "1"),
		boundCustom(crmfilter.KindEnum, "segmento", crmfilter.OpGreaterEq, "1"),
		boundCustom(crmfilter.KindDate, "visita", crmfilter.OpEquals, "2026-01-01"),
		boundCustom(crmfilter.KindBool, "vip", crmfilter.OpIn, "true"),
		boundCustom(crmfilter.KindMultiEnum, "tags", crmfilter.OpContains, "x"),
		boundCustom(crmfilter.KindIDSet, "tags", crmfilter.OpEquals, "x"),
	}
	for _, p := range cases {
		f := crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, p)}}
		if _, _, err := Compile(f, NewOpportunityDescriptor(), 1); !errors.Is(err, ErrUnsupportedOperator) {
			kind, _ := p.BoundKind()
			t.Errorf("kind %v operator %q: Compile() error = %v, want ErrUnsupportedOperator", kind, p.Operator, err)
		}
	}
}

func TestCompileJSONBCustom_RefusesAnObjectWithoutCustomFields(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, boundCustom(crmfilter.KindString, "nota", crmfilter.OpEquals, "a"))}}
	for _, desc := range []ObjectDescriptor{NewConversationDescriptor()} {
		if _, _, err := Compile(f, desc, 1); !errors.Is(err, ErrUnsupportedField) {
			t.Errorf("%s: Compile() error = %v, want ErrUnsupportedField", desc.Object(), err)
		}
	}
}

func TestCompileJSONBCustom_RefusesInvalidBoundValues(t *testing.T) {
	cases := []crmfilter.Predicate{
		boundCustom(crmfilter.KindNumber, "score", crmfilter.OpGreaterEq, "dez"),
		boundCustom(crmfilter.KindDate, "visita", crmfilter.OpAfter, "ontem"),
		boundCustom(crmfilter.KindBool, "vip", crmfilter.OpEquals, "talvez"),
	}
	for _, p := range cases {
		f := crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, p)}}
		if _, _, err := Compile(f, NewOpportunityDescriptor(), 1); err == nil {
			t.Errorf("operator %q values %v: Compile() accepted an invalid value", p.Operator, p.Values)
		}
	}
}
