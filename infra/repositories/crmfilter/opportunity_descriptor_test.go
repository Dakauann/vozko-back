package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

// Golden SQL fragments produced by OpportunityDescriptor (alias "o"). Owner,
// stage, pipeline, etc. are plain columns on the opportunities table, so set
// membership is "= ANY(?)" via compileColumn's OpIn (no join subquery).
const (
	oppOwnerInSQL    = "o.owner_id = ANY(?)"
	oppStageInSQL    = "o.stage_id = ANY(?)"
	oppPipelineEqSQL = "o.pipeline_id = ?"
	oppStatusEqSQL   = "o.status = ?"
	oppValueBetween  = "o.value_cents BETWEEN ? AND ?"
	oppValueGteSQL   = "o.value_cents >= ?"
	oppCloseAfterSQL = "o.close_date > ?"
	oppOwnerEmptySQL = "o.owner_id IS NULL"
	oppQuerySQL      = "o.title ILIKE ?"
	oppCustomEqSQL   = "o.custom_fields->>? = ?"
	oppCustomInSQL   = "o.custom_fields->>? = ANY(?)"
	oppCustomGteSQL  = "(o.custom_fields->>?)::numeric >= ?"
)

// TestOpportunityDescriptor_StandardFields runs standard predicates through the
// shared Compile using the OpportunityDescriptor (which implements
// ObjectDescriptor), proving the column mappings and money-in-cents value column.
func TestOpportunityDescriptor_StandardFields(t *testing.T) {
	desc := NewOpportunityDescriptor()

	tests := []struct {
		name     string
		filter   crmfilter.Filter
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:     "owner IN",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldOwner, crmfilter.OpIn, "u1", "u2"))}},
			wantSQL:  "(" + oppOwnerInSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"u1", "u2"})},
		},
		{
			name:     "stage IN",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldStage, crmfilter.OpIn, "s1"))}},
			wantSQL:  "(" + oppStageInSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"s1"})},
		},
		{
			name:     "pipeline EQ",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldPipeline, crmfilter.OpEquals, "p1"))}},
			wantSQL:  "(" + oppPipelineEqSQL + ")",
			wantArgs: []interface{}{"p1"},
		},
		{
			name:     "status EQ",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldStatus, crmfilter.OpEquals, "won"))}},
			wantSQL:  "(" + oppStatusEqSQL + ")",
			wantArgs: []interface{}{"won"},
		},
		{
			name:     "value BETWEEN (cents)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldValue, crmfilter.OpBetween, "100000", "500000"))}},
			wantSQL:  "(" + oppValueBetween + ")",
			wantArgs: []interface{}{float64(100000), float64(500000)},
		},
		{
			name:     "value GTE (cents)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldValue, crmfilter.OpGreaterEq, "250000"))}},
			wantSQL:  "(" + oppValueGteSQL + ")",
			wantArgs: []interface{}{float64(250000)},
		},
		{
			name:     "close_date AFTER",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldCloseDate, crmfilter.OpAfter, "2026-01-01"))}},
			wantSQL:  "(" + oppCloseAfterSQL + ")",
			wantArgs: []interface{}{mustDate(t, "2026-01-01")},
		},
		{
			name:     "owner IS_EMPTY (sem responsavel)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldOwner, crmfilter.OpIsEmpty))}},
			wantSQL:  "(" + oppOwnerEmptySQL + ")",
			wantArgs: nil,
		},
		{
			name:     "query CONTAINS on title",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldQuery, crmfilter.OpContains, "acme"))}},
			wantSQL:  "(" + oppQuerySQL + ")",
			wantArgs: []interface{}{"%acme%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Both entry points must agree for standard fields.
			for _, entry := range []string{"Compile", "CompileOpportunity"} {
				var gotSQL string
				var gotArgs []interface{}
				var err error
				if entry == "Compile" {
					gotSQL, gotArgs, err = Compile(tt.filter, desc, 1)
				} else {
					gotSQL, gotArgs, err = CompileOpportunity(tt.filter, desc, 1)
				}
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", entry, err)
				}
				if gotSQL != tt.wantSQL {
					t.Errorf("%s SQL mismatch\n got: %s\nwant: %s", entry, gotSQL, tt.wantSQL)
				}
				if strings.Contains(gotSQL, "$1") {
					t.Errorf("%s: expected '?' placeholders, found '$N': %s", entry, gotSQL)
				}
				if len(tt.wantArgs) == 0 {
					if len(gotArgs) != 0 {
						t.Errorf("%s: expected no args, got %#v", entry, gotArgs)
					}
				} else if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
					t.Errorf("%s args mismatch\n got: %#v\nwant: %#v", entry, gotArgs, tt.wantArgs)
				}
			}
		})
	}
}

// TestOpportunityDescriptor_CustomFields proves custom-field predicates compile
// to jsonb-path fragments with the key bound as the leading positional "?".
func TestOpportunityDescriptor_CustomFields(t *testing.T) {
	desc := NewOpportunityDescriptor()

	custom := func(op crmfilter.Operator, key string, values ...string) crmfilter.Predicate {
		return crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: key, Operator: op, Values: values}
	}

	tests := []struct {
		name     string
		pred     crmfilter.Predicate
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:     "custom text EQ",
			pred:     custom(crmfilter.OpEquals, "segmento", "enterprise"),
			wantSQL:  "(" + oppCustomEqSQL + ")",
			wantArgs: []interface{}{"segmento", "enterprise"},
		},
		{
			name:     "custom IN",
			pred:     custom(crmfilter.OpIn, "origem", "whatsapp", "instagram"),
			wantSQL:  "(" + oppCustomInSQL + ")",
			wantArgs: []interface{}{"origem", pq.Array([]string{"whatsapp", "instagram"})},
		},
		{
			name:     "custom numeric GTE",
			pred:     custom(crmfilter.OpGreaterEq, "score", "1000"),
			wantSQL:  "(" + oppCustomGteSQL + ")",
			wantArgs: []interface{}{"score", float64(1000)},
		},
		{
			name:     "custom IS_SET",
			pred:     custom(crmfilter.OpIsSet, "segmento"),
			wantSQL:  "(o.custom_fields->>? IS NOT NULL)",
			wantArgs: []interface{}{"segmento"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, tt.pred)}}
			gotSQL, gotArgs, err := CompileOpportunity(f, desc, 1)
			if err != nil {
				t.Fatalf("CompileOpportunity: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL mismatch\n got: %s\nwant: %s", gotSQL, tt.wantSQL)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args mismatch\n got: %#v\nwant: %#v", gotArgs, tt.wantArgs)
			}
		})
	}
}

// TestOpportunityDescriptor_MixedGroups proves the group AND/OR nesting of
// CompileOpportunity matches Compile when standard and custom predicates mix:
// value >= 100000 AND (custom origem = whatsapp OR status = won).
func TestOpportunityDescriptor_MixedGroups(t *testing.T) {
	desc := NewOpportunityDescriptor()
	f := crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.And, pred(crmfilter.FieldValue, crmfilter.OpGreaterEq, "100000")),
		group(crmfilter.Or,
			crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "origem", Operator: crmfilter.OpEquals, Values: []string{"whatsapp"}},
			pred(crmfilter.FieldStatus, crmfilter.OpEquals, "won"),
		),
	}}
	gotSQL, gotArgs, err := CompileOpportunity(f, desc, 1)
	if err != nil {
		t.Fatalf("CompileOpportunity: %v", err)
	}
	wantSQL := "(" + oppValueGteSQL + ") AND ((" + oppCustomEqSQL + ") OR (" + oppStatusEqSQL + "))"
	if gotSQL != wantSQL {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", gotSQL, wantSQL)
	}
	wantArgs := []interface{}{float64(100000), "origem", "whatsapp", "won"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("args mismatch\n got: %#v\nwant: %#v", gotArgs, wantArgs)
	}
}

// TestOpportunityDescriptor_UnsupportedFields verifies fields with no opportunity
// column (label, conversation-only fields) and custom (via the field-only
// interface) report ErrUnsupportedField.
func TestOpportunityDescriptor_UnsupportedFields(t *testing.T) {
	desc := NewOpportunityDescriptor()
	for _, field := range []crmfilter.Field{
		crmfilter.FieldLabel,
		crmfilter.FieldChannel,
		crmfilter.FieldCampaign,
		crmfilter.FieldUnread,
		crmfilter.FieldWindowOpen,
		crmfilter.FieldLastActivityAt,
		crmfilter.FieldCustom,
	} {
		if _, err := desc.Field(field); !errors.Is(err, ErrUnsupportedField) {
			t.Errorf("field %q: expected ErrUnsupportedField, got %v", field, err)
		}
	}
}

// TestOpportunityDescriptor_EmptyFilter mirrors Compile's empty-filter contract.
func TestOpportunityDescriptor_EmptyFilter(t *testing.T) {
	desc := NewOpportunityDescriptor()
	sql, args, err := CompileOpportunity(crmfilter.Filter{}, desc, 1)
	if err != nil || sql != "" || len(args) != 0 {
		t.Fatalf("expected empty result, got sql=%q args=%#v err=%v", sql, args, err)
	}
}
