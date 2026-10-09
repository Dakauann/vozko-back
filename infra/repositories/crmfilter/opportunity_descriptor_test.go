package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

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
	oppCustomEqSQL   = "o.custom_fields @> jsonb_build_object(?::text, ?::text)"
)

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
			gotSQL, gotArgs, err := Compile(tt.filter, desc, 1)
			if err != nil {
				t.Fatalf("Compile: unexpected error: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL mismatch\n got: %s\nwant: %s", gotSQL, tt.wantSQL)
			}
			if strings.Contains(gotSQL, "$1") {
				t.Errorf("expected '?' placeholders, found '$N': %s", gotSQL)
			}
			if len(tt.wantArgs) == 0 {
				if len(gotArgs) != 0 {
					t.Errorf("expected no args, got %#v", gotArgs)
				}
			} else if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args mismatch\n got: %#v\nwant: %#v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestOpportunityDescriptor_MixedGroups(t *testing.T) {
	desc := NewOpportunityDescriptor()
	f := crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.And, pred(crmfilter.FieldValue, crmfilter.OpGreaterEq, "100000")),
		group(crmfilter.Or,
			boundCustom(crmfilter.KindEnum, "origem", crmfilter.OpEquals, "whatsapp"),
			pred(crmfilter.FieldStatus, crmfilter.OpEquals, "won"),
		),
	}}
	gotSQL, gotArgs, err := Compile(f, desc, 1)
	if err != nil {
		t.Fatalf("Compile: %v", err)
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

func TestOpportunityDescriptor_ExposesItsCustomFieldsColumn(t *testing.T) {
	if got := NewOpportunityDescriptor().CustomFieldsColumn(); got != "o.custom_fields" {
		t.Fatalf("CustomFieldsColumn() = %q", got)
	}
	if got := (OpportunityDescriptor{Alias: "opp"}).CustomFieldsColumn(); got != "opp.custom_fields" {
		t.Fatalf("CustomFieldsColumn() = %q", got)
	}
}

func TestOpportunityDescriptor_EmptyFilter(t *testing.T) {
	desc := NewOpportunityDescriptor()
	sql, args, err := Compile(crmfilter.Filter{}, desc, 1)
	if err != nil || sql != "" || len(args) != 0 {
		t.Fatalf("expected empty result, got sql=%q args=%#v err=%v", sql, args, err)
	}
}
