package crmfilter

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
)

type Style int

const (
	StyleColumn Style = iota
	StyleMembership
	StyleExists
	StyleBool
	StyleText
)

type FieldMapping struct {
	Style Style
	Kind  crmfilter.Kind

	Expr string

	Subject string
	From    string
	Select  string
	Match   string
	Corr    string
	Extra   string

	TrueExpr  string
	FalseExpr string

	Template string
	Params   int

	ExtraArgs []interface{}
}

type ObjectDescriptor interface {
	Object() string
	Field(field crmfilter.Field) (FieldMapping, error)
}

var (
	ErrUnsupportedField    = errors.New("crmfilter: unsupported field for this object")
	ErrUnsupportedOperator = errors.New("crmfilter: operator not supported by field mapping")
)

func inboundMessageTypesSQL() string {
	types := conversation.InboundMessageTypeStrings()
	quoted := make([]string, len(types))
	for i, t := range types {
		quoted[i] = "'" + t + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

const windowOpenSubquery = "(" +
	"SELECT wce_w.id FROM whatsapp_campaign_entries wce_w " +
	"JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id " +
	"JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id AND lmw.business_phone_id = wc_w.business_phone_id " +
	"WHERE wce_w.deleted_at IS NULL AND lmw.last_message_at > NOW() - INTERVAL '24 hours'" +
	")"

type ConversationDescriptor struct {
	EntryAlias       string
	LastActivityExpr string
	WorkspaceID      string
}

func NewConversationDescriptor() ConversationDescriptor {
	return ConversationDescriptor{EntryAlias: "ae", LastActivityExpr: "lm_created_at"}
}

func (d ConversationDescriptor) Object() string { return "conversation" }

func (d ConversationDescriptor) alias() string {
	if d.EntryAlias == "" {
		return "ae"
	}
	return d.EntryAlias
}

func (d ConversationDescriptor) lastActivity() string {
	if d.LastActivityExpr == "" {
		return "lm_created_at"
	}
	return d.LastActivityExpr
}

func (d ConversationDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	entryID := a + ".entry_id"
	entryType := a + ".entry_type"

	extra, extraArgs := d.membershipScope()

	switch field {
	case crmfilter.FieldStage:
		return FieldMapping{
			Style:     StyleMembership,
			Kind:      crmfilter.KindIDSet,
			Subject:   entryID,
			From:      "entry_stages",
			Select:    "entry_id",
			Match:     "stage_id",
			Extra:     extra,
			ExtraArgs: extraArgs,
		}, nil

	case crmfilter.FieldLabel:
		return FieldMapping{
			Style:     StyleMembership,
			Kind:      crmfilter.KindIDSet,
			Subject:   entryID,
			From:      "entry_labels",
			Select:    "entry_id",
			Match:     "label_id",
			Extra:     extra,
			ExtraArgs: extraArgs,
		}, nil

	case crmfilter.FieldSource:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: entryType}, nil

	case crmfilter.FieldCampaign:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: a + ".campaign_id"}, nil

	case crmfilter.FieldOwner:
		return FieldMapping{
			Style: StyleExists,
			Kind:  crmfilter.KindIDSet,
			From:  "inbox_assignments ia",
			Corr:  "ia.entry_id = " + entryID,
			Match: "ia.assigned_user_id",
		}, nil

	case crmfilter.FieldChannel:
		return FieldMapping{
			Style: StyleExists,
			Kind:  crmfilter.KindEnum,
			From:  "conversation_messages cm_ch",
			Corr:  "cm_ch.entry_id = " + entryID + " AND cm_ch.entry_type = " + entryType,
			Match: "cm_ch.channel",
			Extra: "cm_ch.deleted_at IS NULL",
		}, nil

	case crmfilter.FieldStatus:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: a + ".conversation_status"}, nil

	case crmfilter.FieldCreatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: a + ".created_at"}, nil
	case crmfilter.FieldUpdatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: a + ".updated_at"}, nil
	case crmfilter.FieldLastActivityAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: d.lastActivity()}, nil

	case crmfilter.FieldUnread:
		unreadCount := "(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = " + entryID +
			" AND cm4.entry_type = " + entryType +
			" AND cm4.read = false AND cm4.message_type IN " + inboundMessageTypesSQL() +
			" AND cm4.deleted_at IS NULL)"
		return FieldMapping{
			Style:     StyleBool,
			Kind:      crmfilter.KindBool,
			TrueExpr:  unreadCount + " > 0",
			FalseExpr: unreadCount + " = 0",
		}, nil

	case crmfilter.FieldWindowOpen:
		return FieldMapping{
			Style:     StyleBool,
			Kind:      crmfilter.KindBool,
			TrueExpr:  entryID + " IN " + windowOpenSubquery,
			FalseExpr: entryID + " NOT IN " + windowOpenSubquery,
		}, nil

	case crmfilter.FieldQuery:
		tmpl := "(LOWER(COALESCE(l.name, '')) LIKE LOWER(?) OR COALESCE(l.number, '') LIKE ? OR " +
			entryID + " IN (SELECT DISTINCT cm_q.entry_id FROM conversation_messages cm_q WHERE cm_q.entry_id = " +
			entryID + " AND cm_q.entry_type = " + entryType +
			" AND cm_q.deleted_at IS NULL AND LOWER(cm_q.text) LIKE LOWER(?)))"
		return FieldMapping{Style: StyleText, Kind: crmfilter.KindText, Template: tmpl, Params: 3}, nil

	default:
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
	}
}

func (d ConversationDescriptor) membershipScope() (string, []interface{}) {
	if d.WorkspaceID != "" {
		return "workspace_id = ? AND deleted_at IS NULL", []interface{}{d.WorkspaceID}
	}
	return "deleted_at IS NULL", nil
}
