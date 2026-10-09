package crmfilter

import (
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

func TestAStageOrTagFilterStartsFromTheTaggedEntriesOfEachChannel(t *testing.T) {
	cases := []struct {
		field  crmfilter.Field
		table  string
		column string
	}{
		{crmfilter.FieldStage, "entry_stages", "stage_id"},
		{crmfilter.FieldLabel, "entry_labels", "label_id"},
	}
	for _, tc := range cases {
		t.Run(string(tc.field), func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), pred(tc.field, crmfilter.OpIn, "tag-1"))
			tagged := " WHERE tg.entry_type = '%s' AND tg." + tc.column + " = ANY(?) AND tg.workspace_id = ? AND tg.deleted_at IS NULL AND "
			whatsapp := "SELECT wce_e.lead_id FROM " + tc.table + " tg JOIN whatsapp_campaign_entries wce_e ON wce_e.id = tg.entry_id" +
				strings.Replace(tagged, "%s", "whatsapp", 1) + "wce_e.deleted_at IS NULL"
			if !strings.HasPrefix(sql, "((leads.id IN ("+whatsapp+" UNION ALL ") || !strings.HasSuffix(sql, ")) IS TRUE)") {
				t.Fatalf("%s does not look campaign entries up from the tagged rows: %q", tc.field, sql)
			}
			for _, c := range contactChannels {
				conv := c.conversationAlias("e")
				arm := "FROM " + tc.table + " tg JOIN " + c.conversations + " " + conv + " ON " + conv + ".id = tg.entry_id JOIN " + c.contacts
				if !strings.Contains(sql, arm) || !strings.Contains(sql, strings.Replace(tagged, "%s", string(c.entryType), 1)) || !strings.Contains(sql, c.liveRows("e")) {
					t.Fatalf("%s does not look %s up from the tagged rows among live rows: %q", tc.field, c.conversations, sql)
				}
			}
			if strings.Contains(sql, "lead_entries") {
				t.Fatalf("%s still joins the whole entry union: %q", tc.field, sql)
			}
			if strings.Count(sql, "?") != len(args) || len(args) != 2*(1+len(contactChannels)) {
				t.Fatalf("%s binds %d args for %d placeholders", tc.field, len(args), strings.Count(sql, "?"))
			}
			for i := 1; i < len(args); i += 2 {
				if args[i] != testWorkspace {
					t.Fatalf("%s args = %v, want every channel scoped to the workspace", tc.field, args)
				}
			}
		})
	}
}

func TestAStageOrTagFilterKeepsItsOperators(t *testing.T) {
	arms := 1 + len(contactChannels)
	cases := []struct {
		name   string
		pred   crmfilter.Predicate
		prefix string
		args   int
	}{
		{"not in", pred(crmfilter.FieldStage, crmfilter.OpNotIn, "stage-1"), "((leads.id NOT IN (SELECT wce_e.lead_id FROM entry_stages tg JOIN whatsapp_campaign_entries wce_e ON wce_e.id = tg.entry_id WHERE tg.entry_type = 'whatsapp' AND tg.stage_id = ANY(?) AND ", 2 * arms},
		{"any tag", pred(crmfilter.FieldLabel, crmfilter.OpIsSet), "((leads.id IN (SELECT wce_e.lead_id FROM entry_labels tg JOIN whatsapp_campaign_entries wce_e ON wce_e.id = tg.entry_id WHERE tg.entry_type = 'whatsapp' AND tg.workspace_id = ? AND ", arms},
		{"no tag", pred(crmfilter.FieldLabel, crmfilter.OpIsEmpty), "((leads.id NOT IN (SELECT wce_e.lead_id FROM entry_labels tg JOIN whatsapp_campaign_entries wce_e ON wce_e.id = tg.entry_id WHERE tg.entry_type = 'whatsapp' AND tg.workspace_id = ? AND ", arms},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), tc.pred)
			if !strings.HasPrefix(sql, tc.prefix) || len(args) != tc.args || strings.Count(sql, "?") != tc.args {
				t.Fatalf("sql = %q args = %v", sql, args)
			}
		})
	}
}
