// Package crmfilter (infra) compiles a domain crmfilter.Filter into a
// parameterized Postgres WHERE fragment. It is the single SQL surface that
// replaces the two hand-written, duplicated predicate builders in the
// conversation repository (SearchEntriesWithMessages and
// searchEntriesByWorkspace). The domain package describes WHAT to filter; this
// package describes HOW to query a concrete object.
//
// Placeholder style: GORM positional "?" (matching message_repository.go, which
// runs everything through r.db.Raw with "?"), and "= ANY(?)" + pq.Array for id
// sets (matching the existing entry_stages membership subqueries). Args are
// returned in the exact order their "?" appear, so the caller appends them
// positionally to whatever base args its surrounding query already carries.
package crmfilter

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
)

// Style selects how the compiler turns operators into SQL for a field.
type Style int

const (
	// StyleColumn compares a scalar SQL expression directly
	// (Expr = ?, Expr >= ?, Expr IS NULL, ...).
	StyleColumn Style = iota
	// StyleMembership expresses set membership as
	//   Subject IN (SELECT Select FROM From WHERE Match = ANY(?) [AND Extra])
	// mirroring the entry_stages subqueries in message_repository.go.
	StyleMembership
	// StyleExists expresses presence/match as
	//   EXISTS (SELECT 1 FROM From WHERE Corr [AND Match = ANY(?)] [AND Extra])
	// mirroring the inbox_assignments / conversation_messages EXISTS clauses.
	StyleExists
	// StyleBool selects between two ready-made boolean fragments
	// (unread count > 0 / = 0, window-open IN / NOT IN).
	StyleBool
	// StyleText is a free-text template with N "?" placeholders, each bound to
	// the same %pattern% (the lead-name/number/message query predicate).
	StyleText
)

// FieldMapping is the per-field SQL recipe an ObjectDescriptor returns. Only the
// subset of members relevant to Style is read.
type FieldMapping struct {
	Style Style
	Kind  crmfilter.Kind

	// StyleColumn.
	Expr string

	// StyleMembership / StyleExists.
	Subject string // membership subject id expression, e.g. "ae.entry_id"
	From    string // table, optionally aliased, e.g. "entry_stages" or "inbox_assignments ia"
	Select  string // membership: selected column, e.g. "entry_id"
	Match   string // matched column, e.g. "stage_id" or "ia.assigned_user_id"
	Corr    string // exists: correlation predicate, e.g. "ia.entry_id = ae.entry_id"
	Extra   string // extra AND predicate (no leading AND), e.g. "deleted_at IS NULL"

	// StyleBool.
	TrueExpr  string
	FalseExpr string

	// StyleText.
	Template string
	Params   int // count of "?" in Template, each bound to the same %pattern%

	// ExtraArgs are values bound to any "?" placeholders that appear in Extra
	// (StyleMembership / StyleExists). They are appended AFTER the field's own
	// argument (the "= ANY(?)" id-set), matching the left-to-right order the
	// placeholders occur in the emitted subquery. Used to thread the surrounding
	// WorkspaceID into the entry_stages / entry_labels membership scoping.
	ExtraArgs []interface{}
}

// ObjectDescriptor maps each crmfilter.Field onto the SQL for one CRM object
// (conversation entry today, sales opportunity next). Fields that do not apply
// to the object return an error wrapping ErrUnsupportedField.
type ObjectDescriptor interface {
	// Object names the object, for error messages ("conversation").
	Object() string
	// Field resolves a field to its SQL mapping, or ErrUnsupportedField.
	Field(field crmfilter.Field) (FieldMapping, error)
}

var (
	// ErrUnsupportedField is returned by a descriptor for a field that does not
	// apply to its object (e.g. value/close_date on a conversation).
	ErrUnsupportedField = errors.New("crmfilter: unsupported field for this object")
	// ErrUnsupportedOperator is returned when an operator cannot be expressed by
	// a field's mapping style (should not happen for validated filters).
	ErrUnsupportedOperator = errors.New("crmfilter: operator not supported by field mapping")
)

// inboundMessageTypesSQL is the single-sourced SQL tuple of inbound message
// types, read from conversation.InboundMessageTypeStrings() (the same source the
// conversation repository's inboundTypesSQL() helper uses) so the unread /
// window predicates never drift from the domain definition.
func inboundMessageTypesSQL() string {
	types := conversation.InboundMessageTypeStrings()
	quoted := make([]string, len(types))
	for i, t := range types {
		quoted[i] = "'" + t + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

// windowOpenSubquery mirrors the WindowOpen subquery in
// searchEntriesByWorkspace: entries whose lead has a message window opened in
// the last 24h.
const windowOpenSubquery = "(" +
	"SELECT wce_w.id FROM whatsapp_campaign_entries wce_w " +
	"JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id " +
	"JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id AND lmw.business_phone_id = wc_w.business_phone_id " +
	"WHERE wce_w.deleted_at IS NULL AND lmw.last_message_at > NOW() - INTERVAL '24 hours'" +
	")"

// ConversationDescriptor mirrors the conversation predicates currently built
// inline in searchEntriesByWorkspace (the workspace/global board path, alias
// "ae"). Fields that the current conversation SQL has no notion of
// (value, close_date, lost_reason, custom, and the Phase-1 additions
// pipeline/carteira/campaign/label/source) are reported unsupported.
type ConversationDescriptor struct {
	// EntryAlias is the alias of the entry row in the surrounding query. The
	// global board query names it "ae"; entry_id / entry_type read from it.
	EntryAlias string
	// LastActivityExpr is the scalar column used for last-activity date filters,
	// mirroring the current DateFrom/DateTo behaviour on the last message time.
	LastActivityExpr string
	// WorkspaceID, when set, scopes the entry_stages / entry_labels membership
	// subqueries by workspace_id (reconcile note #1). It is left empty by
	// NewConversationDescriptor() and set by the repository at integration, so
	// the golden tests keep asserting the unscoped shape while production queries
	// carry the tenant boundary explicitly.
	WorkspaceID string
}

// NewConversationDescriptor returns the descriptor for the global board query
// (alias "ae", last activity = "lm_created_at").
func NewConversationDescriptor() ConversationDescriptor {
	return ConversationDescriptor{EntryAlias: "ae", LastActivityExpr: "lm_created_at"}
}

// Object implements ObjectDescriptor.
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

// Field implements ObjectDescriptor.
func (d ConversationDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	entryID := a + ".entry_id"
	entryType := a + ".entry_type"

	extra, extraArgs := d.membershipScope()

	switch field {
	case crmfilter.FieldStage:
		// entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) [AND workspace_id = ?] AND deleted_at IS NULL)
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
		// Membership via entry_labels, mirroring entry_stages exactly:
		// entry_id IN (SELECT entry_id FROM entry_labels WHERE label_id = ANY(?) [AND workspace_id = ?] AND deleted_at IS NULL)
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
		// A conversation has no dedicated source column; its origin is the entry
		// type family (voice | whatsapp | support), surfaced on the outer row.
		// This is what backs a "por origem" board/filter for conversations.
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: entryType}, nil

	case crmfilter.FieldCampaign:
		// campaign_id surfaced on the outer row by the integration query (NULL for
		// support entries, which have no campaign).
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: a + ".campaign_id"}, nil

	case crmfilter.FieldOwner:
		// EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = ae.entry_id AND ia.assigned_user_id = ANY(?))
		return FieldMapping{
			Style: StyleExists,
			Kind:  crmfilter.KindIDSet,
			From:  "inbox_assignments ia",
			Corr:  "ia.entry_id = " + entryID,
			Match: "ia.assigned_user_id",
		}, nil

	case crmfilter.FieldChannel:
		// EXISTS (SELECT 1 FROM conversation_messages cm_ch WHERE cm_ch.entry_id = ae.entry_id AND cm_ch.entry_type = ae.entry_type AND cm_ch.channel = ANY(?) AND cm_ch.deleted_at IS NULL)
		return FieldMapping{
			Style: StyleExists,
			Kind:  crmfilter.KindEnum,
			From:  "conversation_messages cm_ch",
			Corr:  "cm_ch.entry_id = " + entryID + " AND cm_ch.entry_type = " + entryType,
			Match: "cm_ch.channel",
			Extra: "cm_ch.deleted_at IS NULL",
		}, nil

	case crmfilter.FieldStatus:
		// Column on the entry row; the current code filters conversation_status
		// per UNION part, so integration must expose it on the entries row.
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
		// value, close_date, lost_reason, custom, pipeline, carteira: not
		// applicable to a conversation entry (opportunity-only or not yet a
		// conversation concept). label / source / campaign are handled above.
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
	}
}

// membershipScope returns the Extra fragment (and its bound args) shared by the
// entry_stages / entry_labels membership subqueries. When WorkspaceID is set it
// threads the tenant boundary into the subquery; otherwise it emits the
// unscoped "deleted_at IS NULL" the golden tests assert.
func (d ConversationDescriptor) membershipScope() (string, []interface{}) {
	if d.WorkspaceID != "" {
		return "workspace_id = ? AND deleted_at IS NULL", []interface{}{d.WorkspaceID}
	}
	return "deleted_at IS NULL", nil
}
