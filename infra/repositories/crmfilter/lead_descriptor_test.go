package crmfilter

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

// The lead object's whole point is that filters, facet counts and sorting all
// compile from one definition. These tests pin the SQL each field emits, so a
// change to a subquery is a change someone had to mean.

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

// A lead's name column is NOT NULL with an empty default. Without NULLIF,
// "leads with no name" would match nothing at all — the filter would look like
// it worked and quietly answer the wrong question.
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

// The free-text box searches the person AND what we remember about them.
// Operators look leads up by half-remembered facts as often as by name, and
// three placeholders must all bind the same pattern.
func TestLeadQuerySearchesNameNumberAndMemories(t *testing.T) {
	sql, args := compileLead(t, leadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "boleto"))

	for _, fragment := range []string{"leads.name ILIKE ?", "leads.number LIKE ?", "lead_memories lm_q", "lm_q.content ILIKE ?"} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("query predicate missing %q in %q", fragment, sql)
		}
	}
	if len(args) != 3 {
		t.Fatalf("query bound %d args, want 3", len(args))
	}
	for i, a := range args {
		if a != "%boleto%" {
			t.Errorf("arg %d = %v, want %%boleto%%", i, a)
		}
	}
}

// is_set / is_empty on the memory category is how "leads we know something
// about" and "leads we know nothing about" are expressed. Both must reduce to
// a membership test with no bound id set.
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

// Soft-deleted memories must never satisfy a memory filter, on any operator:
// a deleted fact is a fact the operator chose to forget.
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
	// The counted expressions the list projects and sorts by must agree.
	if !strings.Contains(leadDesc().MemoryCountExpr(), "deleted_at IS NULL") {
		t.Error("MemoryCountExpr counts soft-deleted memories")
	}
	if !strings.Contains(leadDesc().LastMemoryAtExpr(), "deleted_at IS NULL") {
		t.Error("LastMemoryAtExpr reads soft-deleted memories")
	}
}

// Stages and labels hang off an ENTRY, never a lead, and entries exist on four
// channels. A lead tagged from a Telegram chat must be reachable by its own
// stage filter, which is why the join goes through the union and not straight
// to whatsapp_campaign_entries.
func TestLeadStageResolvesEntriesOnEveryChannel(t *testing.T) {
	sql, _ := compileLead(t, leadDesc(), pred(crmfilter.FieldStage, crmfilter.OpIn, "stage-1"))

	for _, table := range []string{
		"whatsapp_campaign_entries",
		"unofficial_whatsapp_conversations",
		"telegram_conversations",
		"instagram_conversations",
	} {
		if !strings.Contains(sql, table) {
			t.Errorf("stage predicate cannot see %s entries: %q", table, sql)
		}
	}
	if !strings.Contains(sql, "es.stage_id = ANY(?)") {
		t.Errorf("stage predicate does not match on stage_id: %q", sql)
	}
}

// The workspace boundary on the tag membership subqueries is not decoration:
// stage ids are workspace-scoped, and an unscoped subquery would let a guessed
// id from another tenant select rows here.
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

// Every branch of the channel union filters out NULL lead ids. Without that,
// "leads NOT on Instagram" (a NOT IN over a subquery containing a NULL) returns
// the empty set — SQL's most reliable silent-wrong-answer.
func TestLeadChannelUnionExcludesNullLeadIDs(t *testing.T) {
	source := LeadChannelsSource()
	for _, table := range []string{"unofficial_whatsapp_contacts", "telegram_contacts", "instagram_contacts"} {
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

// Last activity is the default sort of the list. It has to consider every
// channel, and both WhatsApp clocks: last_message_at is real conversation
// activity, updated_at also moves on delivery receipts, and the previous
// implementation ranked on updated_at alone.
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
	} {
		if !strings.Contains(expr, fragment) {
			t.Errorf("LastActivityExpr ignores %s: %q", fragment, expr)
		}
	}
}

// A window that closed is not a window with a past expiry date; the boolean and
// the timestamp must come from the same anchor.
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

// A lead has no owner, no pipeline and no deal value. Reporting those as
// unsupported is what keeps a saved conversation view from silently matching
// every lead when it is opened against the wrong object.
func TestLeadRejectsFieldsThatAreNotItsOwn(t *testing.T) {
	for _, field := range []crmfilter.Field{
		crmfilter.FieldOwner,
		crmfilter.FieldCarteira,
		crmfilter.FieldPipeline,
		crmfilter.FieldValue,
		crmfilter.FieldCloseDate,
		crmfilter.FieldLostReason,
		crmfilter.FieldStatus,
		crmfilter.FieldUnread,
		crmfilter.FieldCustom,
	} {
		if _, err := leadDesc().Field(field); !errors.Is(err, ErrUnsupportedField) {
			t.Errorf("Field(%q) error = %v, want ErrUnsupportedField", field, err)
		}
	}
}

// Every field the lead descriptor claims to support must also be a registered
// domain field, or a filter the UI can build would be rejected by validation
// before the descriptor ever sees it.
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

// Groups combine with AND, predicates within a group with OR. The lead list
// leans on this for "stage=Proposta AND (memória=deal OR memória=objeção)".
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
