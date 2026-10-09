package crmfilter

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

func leadDesc() LeadDescriptor { return NewLeadDescriptor() }

func compileLead(t *testing.T, desc LeadDescriptor, preds ...crmfilter.Predicate) (string, []interface{}) {
	t.Helper()
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: preds}}}
	sql, args, err := Compile(f, desc, 0)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return sql, args
}

func TestLeadNameEmptinessUsesNullif(t *testing.T) {
	sql, args := compileLead(t, leadDesc(), pred(crmfilter.FieldName, crmfilter.OpIsEmpty))
	if want := "(NULLIF(leads.name, '') IS NULL)"; sql != want {
		t.Errorf("name is_empty = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("presence predicate bound %d args, want 0", len(args))
	}

	sql, _ = compileLead(t, leadDesc(), pred(crmfilter.FieldName, crmfilter.OpIsSet))
	if !strings.Contains(sql, "NULLIF(leads.name, '') IS NOT NULL") {
		t.Errorf("name is_set = %q, want a NULLIF presence test", sql)
	}
}

func TestLeadMemoryPresence(t *testing.T) {
	sql, args := compileLead(t, leadDesc(), pred(crmfilter.FieldMemoryCategory, crmfilter.OpIsSet))
	want := "(leads.id IN (SELECT lm_c.lead_id FROM lead_memories lm_c WHERE lm_c.deleted_at IS NULL))"
	if sql != want {
		t.Errorf("memory is_set = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("bound %d args, want 0", len(args))
	}

	sql, _ = compileLead(t, leadDesc(), pred(crmfilter.FieldMemoryCategory, crmfilter.OpIsEmpty))
	if !strings.Contains(sql, "leads.id NOT IN (SELECT lm_c.lead_id FROM lead_memories lm_c") {
		t.Errorf("memory is_empty = %q, want a NOT IN membership test", sql)
	}
}

func TestLeadMemoryPredicatesExcludeDeleted(t *testing.T) {
	cases := []struct {
		name string
		pred crmfilter.Predicate
	}{
		{"category", pred(crmfilter.FieldMemoryCategory, crmfilter.OpIn, "deal")},
		{"author", pred(crmfilter.FieldMemoryAuthor, crmfilter.OpIn, "ai")},
		{"text", pred(crmfilter.FieldMemoryText, crmfilter.OpContains, "prazo")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, _ := compileLead(t, leadDesc(), tc.pred)
			if !strings.Contains(sql, "deleted_at IS NULL") {
				t.Errorf("%s predicate does not exclude soft-deleted memories: %q", tc.name, sql)
			}
		})
	}
	if !strings.Contains(leadDesc().MemoryCountExpr(), "deleted_at IS NULL") {
		t.Error("MemoryCountExpr counts soft-deleted memories")
	}
	if !strings.Contains(leadDesc().LastMemoryAtExpr(), "deleted_at IS NULL") {
		t.Error("LastMemoryAtExpr reads soft-deleted memories")
	}
}

func TestLeadStageResolvesEntriesOnEveryChannel(t *testing.T) {
	sql, _ := compileLead(t, leadDesc(), pred(crmfilter.FieldStage, crmfilter.OpIn, "stage-1"))

	for _, table := range []string{
		"whatsapp_campaign_entries",
		"unofficial_whatsapp_conversations",
		"telegram_conversations",
		"instagram_conversations",
		"facebook_conversations",
	} {
		if !strings.Contains(sql, table) {
			t.Errorf("stage predicate cannot see %s entries: %q", table, sql)
		}
	}
	if !strings.Contains(sql, "es.stage_id = ANY(?)") {
		t.Errorf("stage predicate does not match on stage_id: %q", sql)
	}
}

func TestLeadTagMembershipCarriesWorkspaceScope(t *testing.T) {
	desc := LeadDescriptor{Alias: "leads", WorkspaceID: "ws-1"}

	for _, field := range []crmfilter.Field{crmfilter.FieldStage, crmfilter.FieldLabel} {
		sql, args := compileLead(t, desc, pred(field, crmfilter.OpIn, "tag-1"))
		if !strings.Contains(sql, "workspace_id = ?") {
			t.Errorf("%s membership is not workspace-scoped: %q", field, sql)
		}
		if len(args) != 2 || args[1] != "ws-1" {
			t.Errorf("%s args = %v, want the id set followed by the workspace id", field, args)
		}
	}
}

func TestLeadChannelUnionExcludesNullLeadIDs(t *testing.T) {
	source := leadChannelsFrom
	for _, table := range []string{"unofficial_whatsapp_contacts", "telegram_contacts", "instagram_contacts", "facebook_contacts"} {
		idx := strings.Index(source, table)
		if idx < 0 {
			t.Fatalf("channel union missing %s", table)
		}
		if !strings.Contains(source[idx:], "lead_id IS NOT NULL") {
			t.Errorf("%s branch does not exclude NULL lead ids", table)
		}
	}

	sql, args := compileLead(t, leadDesc(), pred(crmfilter.FieldChannel, crmfilter.OpNotIn, "instagram"))
	if !strings.HasPrefix(sql, "(leads.id NOT IN (SELECT lead_id FROM (") {
		t.Errorf("channel not_in = %q, want a NOT IN over the channel union", sql)
	}
	if len(args) != 1 {
		t.Errorf("channel not_in bound %d args, want 1", len(args))
	}
}

func TestLeadLastActivitySpansEveryChannel(t *testing.T) {
	expr := leadDesc().LastActivityExpr()

	if !strings.HasPrefix(expr, "GREATEST(") {
		t.Fatalf("LastActivityExpr should combine the per-channel clocks with GREATEST, got %q", expr)
	}
	for _, fragment := range []string{
		"wce_a.last_message_at",
		"wce_u.updated_at",
		"lead_message_windows",
		"unofficial_whatsapp_conversations",
		"telegram_conversations",
		"instagram_conversations",
		"facebook_conversations",
	} {
		if !strings.Contains(expr, fragment) {
			t.Errorf("LastActivityExpr ignores %s: %q", fragment, expr)
		}
	}
}

func TestLeadWindowExpressions(t *testing.T) {
	d := leadDesc()
	if !strings.Contains(d.WindowOpenExpr(), "NOW() - INTERVAL '24 hours'") {
		t.Errorf("WindowOpenExpr does not use the 24h service window: %q", d.WindowOpenExpr())
	}
	if !strings.Contains(d.WindowExpiresAtExpr(), "+ INTERVAL '24 hours'") {
		t.Errorf("WindowExpiresAtExpr does not project the window end: %q", d.WindowExpiresAtExpr())
	}

	sql, _ := compileLead(t, d, pred(crmfilter.FieldWindowOpen, crmfilter.OpIsFalse))
	if !strings.HasPrefix(sql, "(NOT EXISTS") {
		t.Errorf("window_open is_false = %q, want the negation of the open test", sql)
	}
}

func TestLeadRejectsFieldsThatAreNotItsOwn(t *testing.T) {
	for _, field := range []crmfilter.Field{
		crmfilter.FieldCarteira,
		crmfilter.FieldPipeline,
		crmfilter.FieldValue,
		crmfilter.FieldCloseDate,
		crmfilter.FieldLostReason,
		crmfilter.FieldStatus,
		crmfilter.FieldUnread,
	} {
		if _, err := leadDesc().Field(field); !errors.Is(err, ErrUnsupportedField) {
			t.Errorf("Field(%q) error = %v, want ErrUnsupportedField", field, err)
		}
	}
}

func TestLeadSupportedFieldsAreRegisteredInTheDomain(t *testing.T) {
	supported := []crmfilter.Field{
		crmfilter.FieldName, crmfilter.FieldNumber, crmfilter.FieldAge, crmfilter.FieldBlocked,
		crmfilter.FieldCreatedAt, crmfilter.FieldUpdatedAt, crmfilter.FieldLastActivityAt,
		crmfilter.FieldChannel, crmfilter.FieldCampaign, crmfilter.FieldCampaignStatus,
		crmfilter.FieldCampaignCount, crmfilter.FieldWindowOpen,
		crmfilter.FieldStage, crmfilter.FieldLabel,
		crmfilter.FieldMemoryCategory, crmfilter.FieldMemoryAuthor, crmfilter.FieldMemoryText,
		crmfilter.FieldMemoryCount, crmfilter.FieldMemoryUpdatedAt,
		crmfilter.FieldQuery,
		crmfilter.FieldOwner, crmfilter.FieldSource, crmfilter.FieldID, crmfilter.FieldPhoneAny,
		crmfilter.FieldEmail, crmfilter.FieldNickname, crmfilter.FieldBirthday, crmfilter.FieldBirthDate,
		crmfilter.FieldZip, crmfilter.FieldState, crmfilter.FieldCity, crmfilter.FieldDistrict,
		crmfilter.FieldGeoPrecision, crmfilter.FieldGeoStatus, crmfilter.FieldHasAddress, crmfilter.FieldHasIdentity,
		crmfilter.FieldOptedOut, crmfilter.FieldWhatsAppOptIn, crmfilter.FieldRelationKind,
		crmfilter.FieldRelativesCount, crmfilter.FieldReferredCount, crmfilter.FieldReferredBy,
	}
	for _, field := range supported {
		if _, ok := crmfilter.SpecFor(field); !ok {
			t.Errorf("%q is mapped by the lead descriptor but absent from the domain registry", field)
		}
		if _, err := leadDesc().Field(field); err != nil {
			t.Errorf("Field(%q) = %v, want a mapping", field, err)
		}
	}
}

func TestLeadFilterGroupsCombine(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{
		{Predicates: []crmfilter.Predicate{pred(crmfilter.FieldBlocked, crmfilter.OpIsFalse)}},
		{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{
			pred(crmfilter.FieldMemoryCategory, crmfilter.OpIn, "deal"),
			pred(crmfilter.FieldMemoryCategory, crmfilter.OpIn, "objection"),
		}},
	}}
	sql, args, err := Compile(f, leadDesc(), 0)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !strings.Contains(sql, " AND ((") || !strings.Contains(sql, " OR ") {
		t.Errorf("group combination = %q, want AND across groups and OR within one", sql)
	}
	if len(args) != 2 {
		t.Errorf("bound %d args, want 2", len(args))
	}
}

func TestHasCampaignExprStopsAtFirstRow(t *testing.T) {
	expr := leadDesc().HasCampaignExpr()

	if !strings.HasPrefix(expr, "EXISTS (") {
		t.Errorf("HasCampaignExpr() = %q, want an EXISTS form", expr)
	}
	if strings.Contains(expr, "COUNT(") {
		t.Errorf("HasCampaignExpr() counts rows to answer a presence question: %q", expr)
	}
	if !strings.Contains(expr, "deleted_at IS NULL") {
		t.Errorf("HasCampaignExpr() ignores soft deletes: %q", expr)
	}
	if !strings.Contains(expr, "lead_id = leads.id") {
		t.Errorf("HasCampaignExpr() is not correlated to the lead: %q", expr)
	}
}

func TestHasMemoryExprStopsAtFirstRow(t *testing.T) {
	expr := leadDesc().HasMemoryExpr()

	if !strings.HasPrefix(expr, "EXISTS (") {
		t.Errorf("HasMemoryExpr() = %q, want an EXISTS form", expr)
	}
	if strings.Contains(expr, "COUNT(") {
		t.Errorf("HasMemoryExpr() counts rows to answer a presence question: %q", expr)
	}
	if !strings.Contains(expr, "deleted_at IS NULL") {
		t.Errorf("HasMemoryExpr() ignores soft deletes: %q", expr)
	}
	if !strings.Contains(expr, "lead_id = leads.id") {
		t.Errorf("HasMemoryExpr() is not correlated to the lead: %q", expr)
	}
}

func TestPresenceAndCountExprsShareTheirScope(t *testing.T) {
	d := leadDesc()

	cases := []struct {
		name     string
		presence string
		count    string
		table    string
	}{
		{"campaign", d.HasCampaignExpr(), d.CampaignCountExpr(), "whatsapp_campaign_entries"},
		{"memory", d.HasMemoryExpr(), d.MemoryCountExpr(), "lead_memories"},
	}

	for _, c := range cases {
		if !strings.Contains(c.presence, c.table) || !strings.Contains(c.count, c.table) {
			t.Errorf("%s: presence and count read different tables (%q vs %q)", c.name, c.presence, c.count)
		}
	}
}

func TestOpenWindowLeadIDsSharesTheWindowRule(t *testing.T) {
	want := "SELECT lmw_s.lead_id FROM lead_message_windows lmw_s WHERE lmw_s.last_message_at > NOW() - INTERVAL '24 hours'"
	if got := leadDesc().OpenWindowLeadIDs(); got != want {
		t.Fatalf("OpenWindowLeadIDs() = %q, want %q", got, want)
	}
	if !strings.Contains(leadDesc().WindowOpenExpr(), "NOW() - INTERVAL '24 hours'") {
		t.Fatalf("WindowOpenExpr() = %q", leadDesc().WindowOpenExpr())
	}
}

func TestLeadChannelsInReadsOnlyTheWorkspaceRows(t *testing.T) {
	source, args := LeadChannelsIn("ws-1")
	if !strings.Contains(source, "wce_c.campaign_id IN (SELECT wc_c.id FROM whatsapp_campaigns wc_c WHERE wc_c.workspace_id = ?)") {
		t.Fatalf("campaign entries must be read through the workspace campaigns: %s", source)
	}
	for _, table := range []string{"unofficial_whatsapp_contacts", "telegram_contacts", "instagram_contacts", "facebook_contacts", "webchat_visitors"} {
		idx := strings.Index(source, table)
		if idx < 0 {
			t.Fatalf("channel union missing %s", table)
		}
		if !strings.Contains(source[idx:], "lead_id IS NOT NULL") || !strings.Contains(source[idx:], ".workspace_id = ?") {
			t.Errorf("%s branch must exclude NULL lead ids and stay in the workspace", table)
		}
	}
	if strings.Count(source, "?") != len(args) || len(args) != 1+len(contactChannels) {
		t.Fatalf("%d placeholders for %d args", strings.Count(source, "?"), len(args))
	}
	for _, a := range args {
		if a != "ws-1" {
			t.Fatalf("args = %v, want only the workspace", args)
		}
	}
}
