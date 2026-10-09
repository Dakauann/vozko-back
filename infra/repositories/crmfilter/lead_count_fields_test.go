package crmfilter

import (
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

func TestACountFilterReadsTheWorkspaceRowsOnceInsteadOfCountingPerLead(t *testing.T) {
	campaigns := "SELECT wce_n.lead_id FROM whatsapp_campaign_entries wce_n WHERE wce_n.deleted_at IS NULL AND " +
		"wce_n.campaign_id IN (SELECT wc_n.id FROM whatsapp_campaigns wc_n WHERE wc_n.workspace_id = ?) GROUP BY wce_n.lead_id HAVING "
	memories := "SELECT lm_n.lead_id FROM lead_memories lm_n WHERE lm_n.workspace_id = ? AND lm_n.deleted_at IS NULL GROUP BY lm_n.lead_id HAVING "
	cases := []struct {
		name string
		pred crmfilter.Predicate
		want string
		args []interface{}
	}{
		{"campaigns between two and three", pred(crmfilter.FieldCampaignCount, crmfilter.OpBetween, "2", "3"),
			"((leads.id IN (" + campaigns + "COUNT(*) BETWEEN ? AND ?)) IS TRUE)", []interface{}{testWorkspace, 2.0, 3.0}},
		{"at most three campaigns keeps the leads without any", pred(crmfilter.FieldCampaignCount, crmfilter.OpLessEq, "3"),
			"((leads.id NOT IN (" + campaigns + "NOT (COUNT(*) <= ?))) IS TRUE)", []interface{}{testWorkspace, 3.0}},
		{"no campaign at all", pred(crmfilter.FieldCampaignCount, crmfilter.OpEquals, "0"),
			"((leads.id NOT IN (" + campaigns + "NOT (COUNT(*) = ?))) IS TRUE)", []interface{}{testWorkspace, 0.0}},
		{"two memories or more", pred(crmfilter.FieldMemoryCount, crmfilter.OpGreaterEq, "2"),
			"((leads.id IN (" + memories + "COUNT(*) >= ?)) IS TRUE)", []interface{}{testWorkspace, 2.0}},
		{"a memory count other than one", pred(crmfilter.FieldMemoryCount, crmfilter.OpNotEquals, "1"),
			"((leads.id NOT IN (" + memories + "NOT (COUNT(*) IS DISTINCT FROM ?))) IS TRUE)", []interface{}{testWorkspace, 1.0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), tc.pred)
			if sql != tc.want {
				t.Fatalf("sql = %q\nwant %q", sql, tc.want)
			}
			if strings.Count(sql, "?") != len(args) || len(args) != len(tc.args) {
				t.Fatalf("%d placeholders for args %v, want %v", strings.Count(sql, "?"), args, tc.args)
			}
			for i := range args {
				if args[i] != tc.args[i] {
					t.Fatalf("args = %v, want %v", args, tc.args)
				}
			}
		})
	}
}

func TestACountFilterRefusesAValueItCannotCount(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		pred(crmfilter.FieldCampaignCount, crmfilter.OpGreaterEq, "many"),
	}}}}
	if _, _, err := Compile(f, scopedLeadDesc(), 0); err == nil {
		t.Fatal("a count compared with text compiled")
	}
}

func TestMembershipFiltersOnCampaignEntriesAndMemoriesStayInTheWorkspace(t *testing.T) {
	cases := []struct {
		name  string
		pred  crmfilter.Predicate
		scope string
	}{
		{"send status", pred(crmfilter.FieldCampaignStatus, crmfilter.OpIn, "READ"),
			"wce_s.campaign_id IN (SELECT wc_s.id FROM whatsapp_campaigns wc_s WHERE wc_s.workspace_id = ?)"},
		{"memory category", pred(crmfilter.FieldMemoryCategory, crmfilter.OpIn, "deal"), "lm_c.workspace_id = ?"},
		{"memory author", pred(crmfilter.FieldMemoryAuthor, crmfilter.OpIn, "human"), "lm_a.workspace_id = ?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), tc.pred)
			if !strings.Contains(sql, tc.scope) || !strings.Contains(sql, "deleted_at IS NULL") {
				t.Fatalf("%s reads other workspaces: %q", tc.name, sql)
			}
			if strings.Count(sql, "?") != len(args) || args[len(args)-1] != testWorkspace {
				t.Fatalf("%s args = %v for %q", tc.name, args, sql)
			}
		})
	}
}

func TestCampaignEntriesOfTheWorkspaceAreOneRule(t *testing.T) {
	want := "wce_x.campaign_id IN (SELECT wc_x.id FROM whatsapp_campaigns wc_x WHERE wc_x.workspace_id = ?)"
	if got := CampaignEntriesInWorkspace("wce_x", "wc_x"); got != want {
		t.Fatalf("CampaignEntriesInWorkspace = %q, want %q", got, want)
	}
	source, _ := LeadChannelsIn("ws-1")
	if !strings.Contains(source, CampaignEntriesInWorkspace("wce_c", "wc_c")) {
		t.Fatalf("the channel facet reads campaign entries through another rule: %s", source)
	}
}
