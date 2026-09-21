package crmfilter

import (
	"fmt"

	"vozko/domain/crmfilter"
)

type LeadDescriptor struct {
	Alias       string
	WorkspaceID string
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

func (d LeadDescriptor) WindowOpenExpr() string {
	return "EXISTS (SELECT 1 FROM lead_message_windows lmw_o" +
		" WHERE lmw_o.lead_id = " + d.id() +
		" AND lmw_o.last_message_at > NOW() - INTERVAL '24 hours')"
}

func (d LeadDescriptor) WindowExpiresAtExpr() string {
	return "(" + d.WindowLastMessageExpr() + " + INTERVAL '24 hours')"
}

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

const leadChannelsFrom = "(" +
	"SELECT wce_c.lead_id AS lead_id, 'whatsapp' AS channel FROM whatsapp_campaign_entries wce_c WHERE wce_c.deleted_at IS NULL" +
	" UNION ALL SELECT lmw_c.lead_id, 'whatsapp' FROM lead_message_windows lmw_c" +
	" UNION ALL SELECT uwct_c.lead_id, 'unofficial_whatsapp' FROM unofficial_whatsapp_contacts uwct_c WHERE uwct_c.lead_id IS NOT NULL AND uwct_c.deleted_at IS NULL" +
	" UNION ALL SELECT tgct_c.lead_id, 'telegram' FROM telegram_contacts tgct_c WHERE tgct_c.lead_id IS NOT NULL AND tgct_c.deleted_at IS NULL" +
	" UNION ALL SELECT igct_c.lead_id, 'instagram' FROM instagram_contacts igct_c WHERE igct_c.lead_id IS NOT NULL AND igct_c.deleted_at IS NULL" +
	") lead_channels"

const leadEntriesFrom = "(" +
	"SELECT wce_e.lead_id AS lead_id, wce_e.id AS entry_id, 'whatsapp' AS entry_type FROM whatsapp_campaign_entries wce_e WHERE wce_e.deleted_at IS NULL" +
	" UNION ALL SELECT uwct_e.lead_id, uwc_e.id, 'unofficial_whatsapp' FROM unofficial_whatsapp_conversations uwc_e JOIN unofficial_whatsapp_contacts uwct_e ON uwct_e.id = uwc_e.contact_id WHERE uwct_e.lead_id IS NOT NULL AND uwc_e.deleted_at IS NULL AND uwct_e.deleted_at IS NULL" +
	" UNION ALL SELECT tgct_e.lead_id, tgc_e.id, 'telegram' FROM telegram_conversations tgc_e JOIN telegram_contacts tgct_e ON tgct_e.id = tgc_e.contact_id WHERE tgct_e.lead_id IS NOT NULL AND tgc_e.deleted_at IS NULL AND tgct_e.deleted_at IS NULL" +
	" UNION ALL SELECT igct_e.lead_id, igc_e.id, 'instagram' FROM instagram_conversations igc_e JOIN instagram_contacts igct_e ON igct_e.id = igc_e.contact_id WHERE igct_e.lead_id IS NOT NULL AND igc_e.deleted_at IS NULL AND igct_e.deleted_at IS NULL" +
	") lead_entries"

func LeadChannelsSource() string { return leadChannelsFrom }

func (d LeadDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	col := func(name string) string { return a + "." + name }
	tagExtra, tagArgs := d.tagScope()

	switch field {
	case crmfilter.FieldName:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindString, Expr: "NULLIF(" + col("name") + ", '')"}, nil
	case crmfilter.FieldNumber:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindString, Expr: col("number")}, nil
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
		tmpl := "(" + col("name") + " ILIKE ? OR " + col("number") + " LIKE ?" +
			" OR EXISTS (SELECT 1 FROM lead_memories lm_q WHERE lm_q.lead_id = " + d.id() +
			" AND lm_q.deleted_at IS NULL AND lm_q.content ILIKE ?))"
		return FieldMapping{Style: StyleText, Kind: crmfilter.KindText, Template: tmpl, Params: 3}, nil

	default:
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
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
