package lead

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/infra/database/schema"
)

func TestLeadsFilterByHowAndWhenTheyLeftOnPostgres(t *testing.T) {
	db := leadStoreDB(t)
	if err := db.AutoMigrate(&schema.LeadExitRun{}, &schema.LeadMemory{}, &schema.LeadMessageWindow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ws, other := uuid.NewString(), uuid.NewString()
	leads := map[string]string{}
	insert := func(name, optedOutAt, record string) {
		id := uuid.NewString()
		leads[name] = id
		sql := `INSERT INTO leads (id, workspace_id, name, source, version, opted_out_at, whatsapp_consent_record, created_at, updated_at)
			VALUES (?, ?, ?, 'manual', 1, ` + optedOutAt + `, ` + record + `, now(), now())`
		if err := db.Exec(sql, id, ws, name).Error; err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}
	withdrawal := func(source, eventID string) string {
		return `'{"kind":"withdraw","source":"` + source + `","eventId":"` + eventID + `"}'::jsonb`
	}
	insert("tapped", "'2026-10-05T12:00:00Z'", withdrawal("button_reply", "ev-tap"))
	insert("template word", "'2026-10-06T12:00:00Z'", withdrawal("keyword", "ev-template"))
	insert("workspace word", "'2026-10-07T12:00:00Z'", withdrawal("keyword", "ev-workspace"))
	insert("operator", "'2026-09-01T12:00:00Z'", withdrawal("manual", "ev-operator"))
	insert("before the records", "'2026-10-08T12:00:00Z'", "NULL")
	insert("granted", "NULL", `'{"kind":"grant","source":"manual","eventId":"ev-grant"}'::jsonb`)
	runs := []struct{ workspace, inbound, trigger, eventID string }{
		{ws, "in-template", "template_word", "ev-template"},
		{ws, "in-workspace", "workspace_word", "ev-workspace"},
		{other, "in-elsewhere", "template_word", "ev-workspace"},
	}
	for _, run := range runs {
		err := db.Exec(`INSERT INTO lead_exit_runs (workspace_id, inbound_message_id, exit_trigger, kind, outcome, consent_event_id, claimed_at, updated_at)
			VALUES (?, ?, ?, 'opt_out', 'changed', ?, now(), now())`, run.workspace, run.inbound, run.trigger, run.eventID).Error
		if err != nil {
			t.Fatalf("insert run: %v", err)
		}
	}
	one := func(field crmfilter.Field, op crmfilter.Operator, values ...string) crmfilter.Filter {
		return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: field, Operator: op, Values: values}}}}}
	}
	cases := []struct {
		name   string
		filter crmfilter.Filter
		want   []string
	}{
		{"a template button", one(crmfilter.FieldOptOutSource, crmfilter.OpIn, "button_reply"), []string{"tapped"}},
		{"a template word", one(crmfilter.FieldOptOutSource, crmfilter.OpEquals, "template_word"), []string{"template word"}},
		{"a workspace word, not another workspace's run", one(crmfilter.FieldOptOutSource, crmfilter.OpIn, "workspace_word"), []string{"workspace word"}},
		{"any exit word", one(crmfilter.FieldOptOutSource, crmfilter.OpIn, "keyword"), []string{"template word", "workspace word"}},
		{"a button or an operator", one(crmfilter.FieldOptOutSource, crmfilter.OpIn, "button_reply", "manual"), []string{"tapped", "operator"}},
		{"not by an operator", one(crmfilter.FieldOptOutSource, crmfilter.OpNotIn, "manual"),
			[]string{"tapped", "template word", "workspace word", "before the records", "granted"}},
		{"left in the first half of October", one(crmfilter.FieldOptedOutAt, crmfilter.OpBetween, "2026-10-01", "2026-10-07"),
			[]string{"tapped", "template word"}},
		{"left after September", one(crmfilter.FieldOptedOutAt, crmfilter.OpAfter, "2026-09-30"),
			[]string{"tapped", "template word", "workspace word", "before the records"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.ListWithSummary(lead.ListLeadsInput{WorkspaceID: ws, Filter: tc.filter, Today: time.Date(2026, time.October, 16, 10, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatalf("ListWithSummary() error = %v", err)
			}
			got := map[string]bool{}
			for _, item := range page.Items {
				got[item.Lead.ID] = true
			}
			if page.TotalItems != int64(len(tc.want)) || len(got) != len(tc.want) {
				t.Fatalf("total = %d, items = %d, want %v", page.TotalItems, len(got), tc.want)
			}
			for _, name := range tc.want {
				if !got[leads[name]] {
					t.Errorf("%q is missing", name)
				}
			}
		})
	}
}
