package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

// Golden SQL fragments produced by ConversationDescriptor (alias "ae"). Keeping
// them as named constants documents the exact shape the compiler must emit so it
// can later replace the inline builders in message_repository.go.
const (
	stageInSQL    = "ae.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND deleted_at IS NULL)"
	stageEmptySQL = "ae.entry_id NOT IN (SELECT entry_id FROM entry_stages WHERE deleted_at IS NULL)"
	ownerEqSQL    = "EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = ae.entry_id AND ia.assigned_user_id = ANY(?))"
	ownerEmptySQL = "NOT EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = ae.entry_id)"
	channelEqSQL  = "EXISTS (SELECT 1 FROM conversation_messages cm_ch WHERE cm_ch.entry_id = ae.entry_id AND cm_ch.entry_type = ae.entry_type AND cm_ch.channel = ANY(?) AND cm_ch.deleted_at IS NULL)"
	statusEqSQL   = "ae.conversation_status = ?"
	dateAfterSQL  = "lm_created_at > ?"

	labelInSQL       = "ae.entry_id IN (SELECT entry_id FROM entry_labels WHERE label_id = ANY(?) AND deleted_at IS NULL)"
	labelInScopedSQL = "ae.entry_id IN (SELECT entry_id FROM entry_labels WHERE label_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)"
	stageInScopedSQL = "ae.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)"
	sourceEqSQL      = "ae.entry_type = ?"
	campaignInSQL    = "ae.campaign_id = ANY(?)"
)

func pred(field crmfilter.Field, op crmfilter.Operator, values ...string) crmfilter.Predicate {
	return crmfilter.Predicate{Field: field, Operator: op, Values: values}
}

func group(conj crmfilter.Conjunction, preds ...crmfilter.Predicate) crmfilter.Group {
	return crmfilter.Group{Conjunction: conj, Predicates: preds}
}

func mustDate(t *testing.T, v string) interface{} {
	t.Helper()
	d, err := crmfilter.ParseDate(v)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", v, err)
	}
	return d
}

func TestCompile_GoldenSQL(t *testing.T) {
	desc := NewConversationDescriptor()

	tests := []struct {
		name     string
		filter   crmfilter.Filter
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:     "stage IN",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldStage, crmfilter.OpIn, "s1", "s2"))}},
			wantSQL:  "(" + stageInSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"s1", "s2"})},
		},
		{
			name:     "date AFTER on last activity",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldLastActivityAt, crmfilter.OpAfter, "2026-01-01"))}},
			wantSQL:  "(" + dateAfterSQL + ")",
			wantArgs: []interface{}{mustDate(t, "2026-01-01")},
		},
		{
			name:     "owner EQ",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldOwner, crmfilter.OpEquals, "u1"))}},
			wantSQL:  "(" + ownerEqSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"u1"})},
		},
		{
			// "Sem responsável": the table's unassigned filter must compile to a
			// NOT EXISTS over inbox_assignments with no value args.
			name:     "owner is_empty (unassigned)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldOwner, crmfilter.OpIsEmpty))}},
			wantSQL:  "(" + ownerEmptySQL + ")",
			wantArgs: nil,
		},
		{
			name:     "status EQ",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldStatus, crmfilter.OpEquals, "open"))}},
			wantSQL:  "(" + statusEqSQL + ")",
			wantArgs: []interface{}{"open"},
		},
		{
			name:     "label IN (unscoped)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldLabel, crmfilter.OpIn, "l1", "l2"))}},
			wantSQL:  "(" + labelInSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"l1", "l2"})},
		},
		{
			name:     "source EQ (entry_type)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldSource, crmfilter.OpEquals, "whatsapp"))}},
			wantSQL:  "(" + sourceEqSQL + ")",
			wantArgs: []interface{}{"whatsapp"},
		},
		{
			name:     "campaign IN",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldCampaign, crmfilter.OpIn, "c1", "c2"))}},
			wantSQL:  "(" + campaignInSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"c1", "c2"})},
		},
		{
			name:     "stage is_empty (unstaged)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldStage, crmfilter.OpIsEmpty))}},
			wantSQL:  "(" + stageEmptySQL + ")",
			wantArgs: nil,
		},
		{
			// stage=s1 AND (channel=whatsapp OR channel=instagram):
			// two top-level groups combined with AND, the second an OR group.
			name: "AND of groups with an OR group",
			filter: crmfilter.Filter{Groups: []crmfilter.Group{
				group(crmfilter.And, pred(crmfilter.FieldStage, crmfilter.OpIn, "s1")),
				group(crmfilter.Or,
					pred(crmfilter.FieldChannel, crmfilter.OpEquals, "whatsapp"),
					pred(crmfilter.FieldChannel, crmfilter.OpEquals, "instagram"),
				),
			}},
			wantSQL: "(" + stageInSQL + ") AND ((" + channelEqSQL + ") OR (" + channelEqSQL + "))",
			wantArgs: []interface{}{
				pq.Array([]string{"s1"}),
				pq.Array([]string{"whatsapp"}),
				pq.Array([]string{"instagram"}),
			},
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
			// Placeholder style: GORM "?" positional, never "$N".
			if strings.Contains(gotSQL, "$1") || strings.Contains(gotSQL, "$2") {
				t.Errorf("expected '?' placeholders, found '$N' style: %s", gotSQL)
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

func TestCompile_WorkspaceScopedMembership(t *testing.T) {
	// With WorkspaceID set, entry_stages / entry_labels membership subqueries
	// carry the tenant boundary; the workspace id binds AFTER the id-set array.
	desc := NewConversationDescriptor()
	desc.WorkspaceID = "ws-1"

	cases := []struct {
		name     string
		filter   crmfilter.Filter
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:     "stage IN scoped",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldStage, crmfilter.OpIn, "s1"))}},
			wantSQL:  "(" + stageInScopedSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"s1"}), "ws-1"},
		},
		{
			name:     "label IN scoped",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldLabel, crmfilter.OpIn, "l1"))}},
			wantSQL:  "(" + labelInScopedSQL + ")",
			wantArgs: []interface{}{pq.Array([]string{"l1"}), "ws-1"},
		},
		{
			name:     "label is_empty scoped (only workspace arg)",
			filter:   crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.Or, pred(crmfilter.FieldLabel, crmfilter.OpIsEmpty))}},
			wantSQL:  "(ae.entry_id NOT IN (SELECT entry_id FROM entry_labels WHERE workspace_id = ? AND deleted_at IS NULL))",
			wantArgs: []interface{}{"ws-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSQL, gotArgs, err := Compile(tc.filter, desc, 1)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if gotSQL != tc.wantSQL {
				t.Errorf("SQL mismatch\n got: %s\nwant: %s", gotSQL, tc.wantSQL)
			}
			if !reflect.DeepEqual(gotArgs, tc.wantArgs) {
				t.Errorf("args mismatch\n got: %#v\nwant: %#v", gotArgs, tc.wantArgs)
			}
		})
	}
}

func TestCompile_UnsupportedFieldOnConversation(t *testing.T) {
	desc := NewConversationDescriptor()
	// value BETWEEN validates in the domain but is not a conversation field.
	f := crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.Or, pred(crmfilter.FieldValue, crmfilter.OpBetween, "100", "500")),
	}}
	_, _, err := Compile(f, desc, 1)
	if err == nil {
		t.Fatal("expected error for value BETWEEN on conversation, got nil")
	}
	if !errors.Is(err, ErrUnsupportedField) {
		t.Errorf("expected ErrUnsupportedField, got %v", err)
	}
	// label / source / campaign are now supported on conversations (Phase 1);
	// only the opportunity-only / not-yet-modeled fields remain unsupported.
	for _, field := range []crmfilter.Field{crmfilter.FieldCloseDate, crmfilter.FieldLostReason, crmfilter.FieldCustom, crmfilter.FieldPipeline, crmfilter.FieldCarteira, crmfilter.FieldValue} {
		if _, e := desc.Field(field); !errors.Is(e, ErrUnsupportedField) {
			t.Errorf("field %q: expected ErrUnsupportedField, got %v", field, e)
		}
	}
}

func TestCompile_EmptyFilter(t *testing.T) {
	desc := NewConversationDescriptor()
	sql, args, err := Compile(crmfilter.Filter{}, desc, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sql != "" {
		t.Errorf("expected empty SQL, got %q", sql)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %#v", args)
	}

	// A filter with only empty groups is also empty.
	sql, _, err = Compile(crmfilter.Filter{Groups: []crmfilter.Group{{}}}, desc, 1)
	if err != nil || sql != "" {
		t.Errorf("expected empty result for empty groups, got sql=%q err=%v", sql, err)
	}
}

func TestCompile_InvalidFilterRejected(t *testing.T) {
	desc := NewConversationDescriptor()
	// between requires exactly two values.
	f := crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.Or, pred(crmfilter.FieldValue, crmfilter.OpBetween, "100")),
	}}
	if _, _, err := Compile(f, desc, 1); err == nil {
		t.Fatal("expected validation error for single-value BETWEEN, got nil")
	}
}

func TestCompile_BoolAndText(t *testing.T) {
	desc := NewConversationDescriptor()

	// unread is_true -> count(...) > 0
	sql, args, err := Compile(crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.Or, pred(crmfilter.FieldUnread, crmfilter.OpIsTrue)),
	}}, desc, 1)
	if err != nil {
		t.Fatalf("unread: %v", err)
	}
	if !strings.Contains(sql, "cm4.read = false") || !strings.HasSuffix(sql, "> 0)") {
		t.Errorf("unread is_true SQL unexpected: %s", sql)
	}
	if len(args) != 0 {
		t.Errorf("unread is_true should have no args, got %#v", args)
	}

	// window_open is_false -> entry_id NOT IN (...)
	sql, _, err = Compile(crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.Or, pred(crmfilter.FieldWindowOpen, crmfilter.OpIsFalse)),
	}}, desc, 1)
	if err != nil {
		t.Fatalf("window_open: %v", err)
	}
	if !strings.Contains(sql, "ae.entry_id NOT IN") || !strings.Contains(sql, "lead_message_windows") {
		t.Errorf("window_open is_false SQL unexpected: %s", sql)
	}

	// query contains -> lead name/number/message template, 3 same-pattern args.
	sql, args, err = Compile(crmfilter.Filter{Groups: []crmfilter.Group{
		group(crmfilter.Or, pred(crmfilter.FieldQuery, crmfilter.OpContains, "acme")),
	}}, desc, 1)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if strings.Count(sql, "?") != 3 {
		t.Errorf("query contains should have 3 placeholders: %s", sql)
	}
	want := []interface{}{"%acme%", "%acme%", "%acme%"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("query args mismatch\n got: %#v\nwant: %#v", args, want)
	}
}
