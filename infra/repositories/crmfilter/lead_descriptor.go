package crmfilter

import (
	"fmt"

	"vozko/domain/crmfilter"
)

// LeadDescriptor maps each crmfilter.Field onto the SQL for the leads table
// (alias "leads" by default). It is the third object descriptor, after
// conversation and opportunity, and exists for the same reason they do: the
// leads list, its facet counts and any saved view over leads must all read from
// ONE filter definition. The previous leads endpoint hand-rolled six scalar
// query params into six `if` branches in the repository, which is exactly the
// duplicated predicate builder this package was written to delete.
//
// A lead is a person, so its filterable surface is different from a
// conversation's:
//
//   - identity: name, number, age (+ the free-text query over both, and over
//     what we remember about them)
//   - lifecycle: blocked
//   - reach: which channels the lead exists on, which campaigns touched it and
//     with what delivery outcome, whether its 24h WhatsApp window is open
//   - engagement: created/updated/last-activity clocks, campaign count
//   - knowledge: lead_memory category, author, content, count and freshness
//
// Everything derived (activity, counts) is exposed as an exported expression so
// the repository's SELECT list, its ORDER BY and this descriptor's predicates
// are the same SQL string, never three drifting copies.
type LeadDescriptor struct {
	// Alias is the alias of the leads row in the surrounding query (default
	// "leads", which is also the table name GORM emits unaliased).
	Alias string
	// WorkspaceID, when set, scopes the entry_stages / entry_labels membership
	// subqueries by workspace_id, matching ConversationDescriptor. Left empty
	// by NewLeadDescriptor() so the golden tests assert the unscoped shape.
	WorkspaceID string
}

// NewLeadDescriptor returns the descriptor for the leads list query.
func NewLeadDescriptor() LeadDescriptor { return LeadDescriptor{Alias: "leads"} }

// Object implements ObjectDescriptor.
func (d LeadDescriptor) Object() string { return "lead" }

func (d LeadDescriptor) alias() string {
	if d.Alias == "" {
		return "leads"
	}
	return d.Alias
}

func (d LeadDescriptor) id() string { return d.alias() + ".id" }

// ---------------------------------------------------------------------------
// Derived expressions. Exported because the lead repository projects them as
// result columns and sorts by them; a second hand-written copy over there is
// how "last activity" starts meaning one thing in the sort and another in the
// filter.
// ---------------------------------------------------------------------------

// CampaignCountExpr counts the WhatsApp campaign entries the lead appears in.
func (d LeadDescriptor) CampaignCountExpr() string {
	return "(SELECT COUNT(*) FROM whatsapp_campaign_entries wce_n" +
		" WHERE wce_n.lead_id = " + d.id() + " AND wce_n.deleted_at IS NULL)"
}

// MemoryCountExpr counts the lead's active memories.
func (d LeadDescriptor) MemoryCountExpr() string {
	return "(SELECT COUNT(*) FROM lead_memories lm_n" +
		" WHERE lm_n.lead_id = " + d.id() + " AND lm_n.deleted_at IS NULL)"
}

// LastMemoryAtExpr is when we last learned something about the lead.
func (d LeadDescriptor) LastMemoryAtExpr() string {
	return "(SELECT MAX(lm_t2.updated_at) FROM lead_memories lm_t2" +
		" WHERE lm_t2.lead_id = " + d.id() + " AND lm_t2.deleted_at IS NULL)"
}

// WindowLastMessageExpr is the newest WhatsApp Cloud window anchor for the
// lead, across every business phone it has talked to.
func (d LeadDescriptor) WindowLastMessageExpr() string {
	return "(SELECT MAX(lmw_t.last_message_at) FROM lead_message_windows lmw_t" +
		" WHERE lmw_t.lead_id = " + d.id() + ")"
}

// WindowOpenExpr reports whether any Cloud API 24h service window is still
// open. It is an EXISTS rather than a comparison against
// WindowLastMessageExpr so the index on (lead_id) can stop at the first row.
func (d LeadDescriptor) WindowOpenExpr() string {
	return "EXISTS (SELECT 1 FROM lead_message_windows lmw_o" +
		" WHERE lmw_o.lead_id = " + d.id() +
		" AND lmw_o.last_message_at > NOW() - INTERVAL '24 hours')"
}

// WindowExpiresAtExpr is when the open window closes (NULL when the lead never
// had one). Callers surface it only when WindowOpenExpr is true.
func (d LeadDescriptor) WindowExpiresAtExpr() string {
	return "(" + d.WindowLastMessageExpr() + " + INTERVAL '24 hours')"
}

// LastActivityExpr is the newest moment anything happened with this lead on
// any channel.
//
// GREATEST ignores NULL arguments in Postgres and yields NULL only when every
// argument is NULL, which is exactly the "no activity at all" case the UI
// renders as "—". Both the WhatsApp entry clocks are read: last_message_at is
// real conversation activity, updated_at also moves on delivery/read receipts,
// and the previous implementation ranked on updated_at — dropping it would
// silently re-date every lead that was only ever messaged by a campaign.
func (d LeadDescriptor) LastActivityExpr() string {
	leadID := d.id()
	return "GREATEST(" +
		"(SELECT MAX(wce_a.last_message_at) FROM whatsapp_campaign_entries wce_a WHERE wce_a.lead_id = " + leadID + " AND wce_a.deleted_at IS NULL)," +
		"(SELECT MAX(wce_u.updated_at) FROM whatsapp_campaign_entries wce_u WHERE wce_u.lead_id = " + leadID + " AND wce_u.deleted_at IS NULL)," +
		d.WindowLastMessageExpr() + "," +
		"(SELECT MAX(uwc_a.last_message_at) FROM unofficial_whatsapp_conversations uwc_a JOIN unofficial_whatsapp_contacts uwct_a ON uwct_a.id = uwc_a.contact_id WHERE uwct_a.lead_id = " + leadID + " AND uwc_a.deleted_at IS NULL AND uwct_a.deleted_at IS NULL)," +
		"(SELECT MAX(tgc_a.last_message_at) FROM telegram_conversations tgc_a JOIN telegram_contacts tgct_a ON tgct_a.id = tgc_a.contact_id WHERE tgct_a.lead_id = " + leadID + " AND tgc_a.deleted_at IS NULL AND tgct_a.deleted_at IS NULL)," +
		"(SELECT MAX(igc_a.last_message_at) FROM instagram_conversations igc_a JOIN instagram_contacts igct_a ON igct_a.id = igc_a.contact_id WHERE igct_a.lead_id = " + leadID + " AND igc_a.deleted_at IS NULL AND igct_a.deleted_at IS NULL)" +
		")"
}

// leadChannelsFrom is the derived table of (lead_id, channel) pairs a lead is
// reachable on. Cloud API presence is either a campaign entry or an open/past
// service window; the other three channels bridge through their contact row.
//
// Every branch filters lead_id IS NOT NULL so the NOT IN form of the
// membership predicate ("leads NOT on Instagram") is not silently emptied by a
// NULL in the subquery.
const leadChannelsFrom = "(" +
	"SELECT wce_c.lead_id AS lead_id, 'whatsapp' AS channel FROM whatsapp_campaign_entries wce_c WHERE wce_c.deleted_at IS NULL" +
	" UNION ALL SELECT lmw_c.lead_id, 'whatsapp' FROM lead_message_windows lmw_c" +
	" UNION ALL SELECT uwct_c.lead_id, 'unofficial_whatsapp' FROM unofficial_whatsapp_contacts uwct_c WHERE uwct_c.lead_id IS NOT NULL AND uwct_c.deleted_at IS NULL" +
	" UNION ALL SELECT tgct_c.lead_id, 'telegram' FROM telegram_contacts tgct_c WHERE tgct_c.lead_id IS NOT NULL AND tgct_c.deleted_at IS NULL" +
	" UNION ALL SELECT igct_c.lead_id, 'instagram' FROM instagram_contacts igct_c WHERE igct_c.lead_id IS NOT NULL AND igct_c.deleted_at IS NULL" +
	") lead_channels"

// leadEntriesFrom is the derived table of (lead_id, entry_id, entry_type) the
// CRM tag tables key on. Stages and labels are attached to an ENTRY, never to a
// lead, so filtering leads by stage means resolving the lead's entries first —
// on every channel, not just the Cloud API one, or a lead tagged from a
// Telegram chat would be invisible to its own stage filter.
const leadEntriesFrom = "(" +
	"SELECT wce_e.lead_id AS lead_id, wce_e.id AS entry_id, 'whatsapp' AS entry_type FROM whatsapp_campaign_entries wce_e WHERE wce_e.deleted_at IS NULL" +
	" UNION ALL SELECT uwct_e.lead_id, uwc_e.id, 'unofficial_whatsapp' FROM unofficial_whatsapp_conversations uwc_e JOIN unofficial_whatsapp_contacts uwct_e ON uwct_e.id = uwc_e.contact_id WHERE uwct_e.lead_id IS NOT NULL AND uwc_e.deleted_at IS NULL AND uwct_e.deleted_at IS NULL" +
	" UNION ALL SELECT tgct_e.lead_id, tgc_e.id, 'telegram' FROM telegram_conversations tgc_e JOIN telegram_contacts tgct_e ON tgct_e.id = tgc_e.contact_id WHERE tgct_e.lead_id IS NOT NULL AND tgc_e.deleted_at IS NULL AND tgct_e.deleted_at IS NULL" +
	" UNION ALL SELECT igct_e.lead_id, igc_e.id, 'instagram' FROM instagram_conversations igc_e JOIN instagram_contacts igct_e ON igct_e.id = igc_e.contact_id WHERE igct_e.lead_id IS NOT NULL AND igc_e.deleted_at IS NULL AND igct_e.deleted_at IS NULL" +
	") lead_entries"

// LeadChannelsSource exposes the (lead_id, channel) derived table so callers
// that aggregate over the same filtered set — the facet counts beside the
// channel filter — group by the exact definition the predicate filters by.
func LeadChannelsSource() string { return leadChannelsFrom }

// Field implements ObjectDescriptor.
func (d LeadDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	col := func(name string) string { return a + "." + name }
	tagExtra, tagArgs := d.tagScope()

	switch field {
	// ── identity ────────────────────────────────────────────────────────────
	case crmfilter.FieldName:
		// NULLIF, not the bare column: `name` is NOT NULL with a '' default, so
		// without it "leads with no name" (is_empty) would match nothing and
		// "leads with a name" (is_set) would match everything.
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindString, Expr: "NULLIF(" + col("name") + ", '')"}, nil
	case crmfilter.FieldNumber:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindString, Expr: col("number")}, nil
	case crmfilter.FieldAge:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: col("age")}, nil

	// ── lifecycle ───────────────────────────────────────────────────────────
	case crmfilter.FieldBlocked:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindBool, Expr: col("blocked")}, nil

	// ── clocks ──────────────────────────────────────────────────────────────
	case crmfilter.FieldCreatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("created_at")}, nil
	case crmfilter.FieldUpdatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("updated_at")}, nil
	case crmfilter.FieldLastActivityAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: d.LastActivityExpr()}, nil

	// ── reach ───────────────────────────────────────────────────────────────
	case crmfilter.FieldChannel:
		return FieldMapping{
			Style:   StyleMembership,
			Kind:    crmfilter.KindEnum,
			Subject: d.id(),
			From:    leadChannelsFrom,
			Select:  "lead_id",
			Match:   "channel",
		}, nil

	case crmfilter.FieldCampaign:
		return FieldMapping{
			Style:   StyleMembership,
			Kind:    crmfilter.KindIDSet,
			Subject: d.id(),
			From:    "whatsapp_campaign_entries wce_f",
			Select:  "wce_f.lead_id",
			Match:   "wce_f.campaign_id",
			Extra:   "wce_f.deleted_at IS NULL",
		}, nil

	case crmfilter.FieldCampaignStatus:
		return FieldMapping{
			Style:   StyleMembership,
			Kind:    crmfilter.KindEnum,
			Subject: d.id(),
			From:    "whatsapp_campaign_entries wce_s",
			Select:  "wce_s.lead_id",
			Match:   "wce_s.status",
			Extra:   "wce_s.deleted_at IS NULL",
		}, nil

	case crmfilter.FieldCampaignCount:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: d.CampaignCountExpr()}, nil

	case crmfilter.FieldWindowOpen:
		open := d.WindowOpenExpr()
		return FieldMapping{
			Style:     StyleBool,
			Kind:      crmfilter.KindBool,
			TrueExpr:  open,
			FalseExpr: "NOT " + open,
		}, nil

	// ── CRM tags, resolved through the lead's entries on every channel ───────
	case crmfilter.FieldStage:
		return FieldMapping{
			Style:     StyleMembership,
			Kind:      crmfilter.KindIDSet,
			Subject:   d.id(),
			From:      "entry_stages es JOIN " + leadEntriesFrom + " ON lead_entries.entry_id = es.entry_id AND lead_entries.entry_type = es.entry_type",
			Select:    "lead_entries.lead_id",
			Match:     "es.stage_id",
			Extra:     tagExtra("es"),
			ExtraArgs: tagArgs,
		}, nil

	case crmfilter.FieldLabel:
		return FieldMapping{
			Style:     StyleMembership,
			Kind:      crmfilter.KindIDSet,
			Subject:   d.id(),
			From:      "entry_labels el JOIN " + leadEntriesFrom + " ON lead_entries.entry_id = el.entry_id AND lead_entries.entry_type = el.entry_type",
			Select:    "lead_entries.lead_id",
			Match:     "el.label_id",
			Extra:     tagExtra("el"),
			ExtraArgs: tagArgs,
		}, nil

	// ── knowledge (lead_memory) ─────────────────────────────────────────────
	case crmfilter.FieldMemoryCategory:
		// is_set / is_empty on this field are the "has any memory at all" /
		// "we know nothing about this lead" segments, for free.
		return FieldMapping{
			Style:   StyleMembership,
			Kind:    crmfilter.KindEnum,
			Subject: d.id(),
			From:    "lead_memories lm_c",
			Select:  "lm_c.lead_id",
			Match:   "lm_c.category",
			Extra:   "lm_c.deleted_at IS NULL",
		}, nil

	case crmfilter.FieldMemoryAuthor:
		return FieldMapping{
			Style:   StyleMembership,
			Kind:    crmfilter.KindEnum,
			Subject: d.id(),
			From:    "lead_memories lm_a",
			Select:  "lm_a.lead_id",
			Match:   "lm_a.actor_kind",
			Extra:   "lm_a.deleted_at IS NULL",
		}, nil

	case crmfilter.FieldMemoryText:
		return FieldMapping{
			Style: StyleText,
			Kind:  crmfilter.KindText,
			Template: "EXISTS (SELECT 1 FROM lead_memories lm_x WHERE lm_x.lead_id = " + d.id() +
				" AND lm_x.deleted_at IS NULL AND lm_x.content ILIKE ?)",
			Params: 1,
		}, nil

	case crmfilter.FieldMemoryCount:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: d.MemoryCountExpr()}, nil

	case crmfilter.FieldMemoryUpdatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: d.LastMemoryAtExpr()}, nil

	// ── free text ───────────────────────────────────────────────────────────
	case crmfilter.FieldQuery:
		// One box, three haystacks: the name, the phone number, and what we
		// remember about the person. Operators search leads by half-remembered
		// facts ("boleto", "prefere manhã") as often as by name.
		tmpl := "(" + col("name") + " ILIKE ? OR " + col("number") + " LIKE ?" +
			" OR EXISTS (SELECT 1 FROM lead_memories lm_q WHERE lm_q.lead_id = " + d.id() +
			" AND lm_q.deleted_at IS NULL AND lm_q.content ILIKE ?))"
		return FieldMapping{Style: StyleText, Kind: crmfilter.KindText, Template: tmpl, Params: 3}, nil

	default:
		// owner/carteira/pipeline/value/close_date/lost_reason/source/status/
		// unread/custom: a lead has no owner, no pipeline and no deal value;
		// status and unread belong to its conversations, which the CRM board
		// filters with the conversation descriptor.
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
	}
}

// tagScope returns the Extra fragment (and its bound args) for the
// entry_stages / entry_labels membership subqueries, mirroring
// ConversationDescriptor.membershipScope: scoped by workspace when the
// repository set one, unscoped in the golden tests.
func (d LeadDescriptor) tagScope() (func(alias string) string, []interface{}) {
	if d.WorkspaceID != "" {
		return func(alias string) string {
			return alias + ".workspace_id = ? AND " + alias + ".deleted_at IS NULL"
		}, []interface{}{d.WorkspaceID}
	}
	return func(alias string) string { return alias + ".deleted_at IS NULL" }, nil
}
