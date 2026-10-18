package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
)

const (
	withdrawn   = "leads.opted_out_at IS NOT NULL AND (leads.whatsapp_consent_record ->> 'kind') = ?"
	bySource    = "(leads.whatsapp_consent_record ->> 'source') = ANY(?)"
	byExitWords = "(leads.whatsapp_consent_record ->> 'eventId') IN (SELECT ler_f.consent_event_id FROM lead_exit_runs ler_f" +
		" WHERE ler_f.workspace_id = ? AND ler_f.exit_trigger = ANY(?) AND ler_f.consent_event_id <> '')"
)

func TestLeadOptOutFields_GoldenSQL(t *testing.T) {
	cases := []struct {
		name     string
		pred     crmfilter.Predicate
		wantSQL  string
		wantArgs []interface{}
	}{
		{"left by a button or an agent", pred(crmfilter.FieldOptOutSource, crmfilter.OpIn, "button_reply", "ai_agent"),
			"(" + withdrawn + " AND (" + bySource + "))",
			[]interface{}{"withdraw", pq.Array([]string{"button_reply", "ai_agent"})}},
		{"left by a template word", pred(crmfilter.FieldOptOutSource, crmfilter.OpEquals, "template_word"),
			"(" + withdrawn + " AND (" + byExitWords + "))",
			[]interface{}{"withdraw", testWorkspace, pq.Array([]string{"template_word"})}},
		{"left by a workflow or a workspace word", pred(crmfilter.FieldOptOutSource, crmfilter.OpIn, "workflow", "workspace_word"),
			"(" + withdrawn + " AND (" + bySource + " OR " + byExitWords + "))",
			[]interface{}{"withdraw", pq.Array([]string{"workflow"}), testWorkspace, pq.Array([]string{"workspace_word"})}},
		{"did not leave by an operator", pred(crmfilter.FieldOptOutSource, crmfilter.OpNotIn, "manual"),
			"NOT COALESCE((" + withdrawn + " AND (" + bySource + ")), false)",
			[]interface{}{"withdraw", pq.Array([]string{"manual"})}},
		{"left in a date range", pred(crmfilter.FieldOptedOutAt, crmfilter.OpBetween, "2026-10-01", "2026-10-16"),
			"leads.opted_out_at BETWEEN ? AND ?", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), tc.pred)
			if want := "(" + tc.wantSQL + ")"; sql != want {
				t.Fatalf("SQL\n got: %s\nwant: %s", sql, want)
			}
			if got := strings.Count(sql, "?"); got != len(args) {
				t.Fatalf("%d placeholders for %d args in %s", got, len(args), sql)
			}
			if tc.wantArgs != nil && !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}
}

func TestTheExitWordOriginsNeedTheWorkspace(t *testing.T) {
	desc := scopedLeadDesc()
	desc.WorkspaceID = ""
	mapping, err := desc.Field(crmfilter.FieldOptOutSource)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mapping.Compile(pred(crmfilter.FieldOptOutSource, crmfilter.OpIn, "workspace_word")); !errors.Is(err, ErrWorkspaceScopeRequired) {
		t.Fatalf("got %v, want ErrWorkspaceScopeRequired", err)
	}
}

func TestAnUnknownOptOutOriginCannotMatch(t *testing.T) {
	mapping, err := scopedLeadDesc().Field(crmfilter.FieldOptOutSource)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mapping.Compile(pred(crmfilter.FieldOptOutSource, crmfilter.OpIn, "operator")); !errors.Is(err, crmfilter.ErrInvalidValue) {
		t.Fatalf("got %v, want ErrInvalidValue", err)
	}
}

func TestTheOptOutFieldsAreMappedForLeads(t *testing.T) {
	for _, field := range []crmfilter.Field{crmfilter.FieldOptOutSource, crmfilter.FieldOptedOutAt} {
		if _, ok := crmfilter.SpecFor(field); !ok {
			t.Errorf("%q is absent from the domain registry", field)
		}
		if _, err := leadDesc().Field(field); err != nil {
			t.Errorf("Field(%q) = %v, want a mapping", field, err)
		}
	}
}
