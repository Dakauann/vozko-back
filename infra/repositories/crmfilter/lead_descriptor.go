package crmfilter

import (
	"strings"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

type LeadDescriptor struct {
	Alias       string
	WorkspaceID string
	Today       time.Time
}

func NewLeadDescriptor() LeadDescriptor { return LeadDescriptor{Alias: "leads"} }

func (d LeadDescriptor) Object() string { return "lead" }

func (d LeadDescriptor) alias() string {
	if d.Alias == "" {
		return "leads"
	}
	return d.Alias
}

func (d LeadDescriptor) id() string { return d.alias() + ".id" }

func (d LeadDescriptor) CampaignCountExpr() string {
	return "(SELECT COUNT(*) FROM whatsapp_campaign_entries wce_n" +
		" WHERE wce_n.lead_id = " + d.id() + " AND wce_n.deleted_at IS NULL)"
}

func (d LeadDescriptor) HasCampaignExpr() string {
	return "EXISTS (SELECT 1 FROM whatsapp_campaign_entries wce_p" +
		" WHERE wce_p.lead_id = " + d.id() + " AND wce_p.deleted_at IS NULL)"
}

func (d LeadDescriptor) HasMemoryExpr() string {
	return "EXISTS (SELECT 1 FROM lead_memories lm_p" +
		" WHERE lm_p.lead_id = " + d.id() + " AND lm_p.deleted_at IS NULL)"
}

func (d LeadDescriptor) MemoryCountExpr() string {
	return "(SELECT COUNT(*) FROM lead_memories lm_n" +
		" WHERE lm_n.lead_id = " + d.id() + " AND lm_n.deleted_at IS NULL)"
}

func (d LeadDescriptor) LastMemoryAtExpr() string {
	return "(SELECT MAX(lm_t2.updated_at) FROM lead_memories lm_t2" +
		" WHERE lm_t2.lead_id = " + d.id() + " AND lm_t2.deleted_at IS NULL)"
}

func (d LeadDescriptor) WindowLastMessageExpr() string {
	return "(SELECT MAX(lmw_t.last_message_at) FROM lead_message_windows lmw_t" +
		" WHERE lmw_t.lead_id = " + d.id() + ")"
}

const openWindowSince = "NOW() - INTERVAL '24 hours'"

func (d LeadDescriptor) WindowOpenExpr() string {
	return "EXISTS (SELECT 1 FROM lead_message_windows lmw_o" +
		" WHERE lmw_o.lead_id = " + d.id() +
		" AND lmw_o.last_message_at > " + openWindowSince + ")"
}

func (d LeadDescriptor) OpenWindowLeadIDs() string {
	return "SELECT lmw_s.lead_id FROM lead_message_windows lmw_s WHERE lmw_s.last_message_at > " + openWindowSince
}

func (d LeadDescriptor) WindowExpiresAtExpr() string {
	return "(" + d.WindowLastMessageExpr() + " + INTERVAL '24 hours')"
}

func (d LeadDescriptor) LastActivityExpr() string {
	leadID := d.id()
	clocks := make([]string, 0, len(contactChannels))
	for _, c := range contactChannels {
		clocks = append(clocks, c.lastMessageExpr(leadID))
	}
	return "GREATEST(" +
		"(SELECT MAX(wce_a.last_message_at) FROM whatsapp_campaign_entries wce_a WHERE wce_a.lead_id = " + leadID + " AND wce_a.deleted_at IS NULL)," +
		"(SELECT MAX(wce_u.updated_at) FROM whatsapp_campaign_entries wce_u WHERE wce_u.lead_id = " + leadID + " AND wce_u.deleted_at IS NULL)," +
		d.WindowLastMessageExpr() + "," +
		strings.Join(clocks, ",") +
		")"
}

type contactChannel struct {
	entryType     shared.EntryType
	alias         string
	conversations string
	contacts      string
	contactColumn string
}

var contactChannels = []contactChannel{
	{shared.EntryTypeUnofficialWhatsApp, "uw", "unofficial_whatsapp_conversations", "unofficial_whatsapp_contacts", "contact_id"},
	{shared.EntryTypeTelegram, "tg", "telegram_conversations", "telegram_contacts", "contact_id"},
	{shared.EntryTypeInstagram, "ig", "instagram_conversations", "instagram_contacts", "contact_id"},
	{shared.EntryTypeFacebook, "fb", "facebook_conversations", "facebook_contacts", "contact_id"},
	{shared.EntryTypeWebchat, "wc", "webchat_conversations", "webchat_visitors", "visitor_id"},
}

func (c contactChannel) conversationAlias(suffix string) string { return c.alias + "c_" + suffix }

func (c contactChannel) contactAlias(suffix string) string { return c.alias + "ct_" + suffix }

func (c contactChannel) conversationsWithContacts(suffix string) string {
	conv, contact := c.conversationAlias(suffix), c.contactAlias(suffix)
	return c.conversations + " " + conv + " JOIN " + c.contacts + " " + contact + " ON " + contact + ".id = " + conv + "." + c.contactColumn
}

func (c contactChannel) liveRows(suffix string) string {
	return c.conversationAlias(suffix) + ".deleted_at IS NULL AND " + c.contactAlias(suffix) + ".deleted_at IS NULL"
}

func (c contactChannel) lastMessageExpr(leadID string) string {
	return "(SELECT MAX(" + c.conversationAlias("a") + ".last_message_at) FROM " + c.conversationsWithContacts("a") +
		" WHERE " + c.contactAlias("a") + ".lead_id = " + leadID + " AND " + c.liveRows("a") + ")"
}

func (c contactChannel) channelRow() string {
	contact := c.contactAlias("c")
	return " UNION ALL SELECT " + contact + ".lead_id, '" + string(c.entryType) + "' FROM " + c.contacts + " " + contact +
		" WHERE " + contact + ".lead_id IS NOT NULL AND " + contact + ".deleted_at IS NULL"
}

func (c contactChannel) entryRow() string {
	contact := c.contactAlias("e")
	return " UNION ALL SELECT " + contact + ".lead_id, " + c.conversationAlias("e") + ".id, '" + string(c.entryType) + "' FROM " +
		c.conversationsWithContacts("e") + " WHERE " + contact + ".lead_id IS NOT NULL AND " + c.liveRows("e")
}

func unionOver(head string, row func(contactChannel) string, alias string) string {
	var b strings.Builder
	b.WriteString("(" + head)
	for _, c := range contactChannels {
		b.WriteString(row(c))
	}
	b.WriteString(") " + alias)
	return b.String()
}

var leadChannelsFrom = unionOver(
	"SELECT wce_c.lead_id AS lead_id, 'whatsapp' AS channel FROM whatsapp_campaign_entries wce_c WHERE wce_c.deleted_at IS NULL"+
		" UNION ALL SELECT lmw_c.lead_id, 'whatsapp' FROM lead_message_windows lmw_c",
	contactChannel.channelRow, "lead_channels")

var leadEntriesFrom = unionOver(
	"SELECT wce_e.lead_id AS lead_id, wce_e.id AS entry_id, 'whatsapp' AS entry_type FROM whatsapp_campaign_entries wce_e WHERE wce_e.deleted_at IS NULL",
	contactChannel.entryRow, "lead_entries")

func (c contactChannel) channelRowIn() string {
	return c.channelRow() + " AND " + c.contactAlias("c") + ".workspace_id = ?"
}

var leadChannelsInWorkspace = unionOver(
	"SELECT wce_c.lead_id AS lead_id, 'whatsapp' AS channel FROM whatsapp_campaign_entries wce_c WHERE wce_c.deleted_at IS NULL"+
		" AND wce_c.campaign_id IN (SELECT wc_c.id FROM whatsapp_campaigns wc_c WHERE wc_c.workspace_id = ?)"+
		" UNION ALL SELECT lmw_c.lead_id, 'whatsapp' FROM lead_message_windows lmw_c",
	contactChannel.channelRowIn, "lead_channels")

func LeadChannelsIn(workspaceID string) (string, []interface{}) {
	args := make([]interface{}, 0, 1+len(contactChannels))
	for range 1 + len(contactChannels) {
		args = append(args, workspaceID)
	}
	return leadChannelsInWorkspace, args
}

func LeadEntriesSource() string { return leadEntriesFrom }

func (d LeadDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	col := func(name string) string { return a + "." + name }
	tagExtra, tagArgs := d.tagScope()

	switch field {
	case crmfilter.FieldName:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindString, Expr: "NULLIF(" + col("name") + ", '')"}, nil
	case crmfilter.FieldNumber:
		return FieldMapping{Style: StyleCompiled, Kind: crmfilter.KindString, Compile: d.compileNumber}, nil
	case crmfilter.FieldAge:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: col("age")}, nil

	case crmfilter.FieldBlocked:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindBool, Expr: col("blocked")}, nil

	case crmfilter.FieldCreatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("created_at")}, nil
	case crmfilter.FieldUpdatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("updated_at")}, nil
	case crmfilter.FieldLastActivityAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: d.LastActivityExpr()}, nil

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

	case crmfilter.FieldMemoryCategory:
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

	case crmfilter.FieldQuery:
		return FieldMapping{Style: StyleCompiled, Kind: crmfilter.KindText, Compile: d.compileQuery}, nil

	case crmfilter.FieldArea:
		return d.areaField()

	default:
		return d.recordField(field)
	}
}

func (d LeadDescriptor) tagScope() (func(alias string) string, []interface{}) {
	if d.WorkspaceID != "" {
		return func(alias string) string {
			return alias + ".workspace_id = ? AND " + alias + ".deleted_at IS NULL"
		}, []interface{}{d.WorkspaceID}
	}
	return func(alias string) string { return alias + ".deleted_at IS NULL" }, nil
}
