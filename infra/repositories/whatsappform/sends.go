package whatsappform_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const sendColumns = "s.id::text AS id, s.workspace_id::text AS workspace_id, s.form_id::text AS form_id, s.version_id::text AS version_id," +
	" s.publication_id::text AS publication_id, s.flow_token, s.origin, s.origin_ref, s.entry_id::text AS entry_id, s.entry_type," +
	" s.conversation_id, s.lead_id::text AS lead_id, s.business_phone_id::text AS business_phone_id, s.contact_number, s.message_id," +
	" s.template_id::text AS template_id, s.workflow_run_id::text AS workflow_run_id, s.sent_by::text AS sent_by, s.status, s.failure_reason," +
	" s.created_at, s.sent_at, s.responded_at"

const (
	insertSendSQL = "INSERT INTO whatsapp_form_sends (id, workspace_id, form_id, version_id, publication_id, flow_token, origin, origin_ref, entry_id, entry_type," +
		" conversation_id, lead_id, business_phone_id, contact_number, message_id, template_id, workflow_run_id, sent_by, status, failure_reason," +
		" created_at, sent_at, responded_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	updateSendSQL = "UPDATE whatsapp_form_sends SET status = ?, message_id = ?, failure_reason = ?, sent_at = ?, responded_at = ? WHERE workspace_id = ? AND id = ?"

	sendsFromSQL = "SELECT " + sendColumns + " FROM whatsapp_form_sends s"

	findSendByTokenSQL = sendsFromSQL + " WHERE s.flow_token = ?"

	findSendByMessageSQL = sendsFromSQL + " WHERE s.message_id = ? ORDER BY s.created_at DESC, s.id DESC LIMIT 1"
)

type SendStore struct {
	db *gorm.DB
}

var _ whatsappform.SendRepository = (*SendStore)(nil)

func NewSendStore(db *gorm.DB) *SendStore {
	return &SendStore{db: db}
}

type sendRow struct {
	ID              string
	WorkspaceID     string
	FormID          string
	VersionID       string
	PublicationID   string
	FlowToken       string
	Origin          string
	OriginRef       string
	EntryID         schema.OptionalText
	EntryType       string
	ConversationID  string
	LeadID          schema.OptionalText
	BusinessPhoneID string
	ContactNumber   string
	MessageID       schema.OptionalText
	TemplateID      schema.OptionalText
	WorkflowRunID   schema.OptionalText
	SentBy          schema.OptionalText
	Status          string
	FailureReason   string
	CreatedAt       time.Time
	SentAt          *time.Time
	RespondedAt     *time.Time
}

func (r sendRow) toDomain() whatsappform.Send {
	return whatsappform.Send{
		ID: r.ID, WorkspaceID: r.WorkspaceID, FormID: r.FormID, VersionID: r.VersionID, PublicationID: r.PublicationID, Token: r.FlowToken,
		Origin: whatsappform.SendOrigin(r.Origin), OriginRef: r.OriginRef, EntryID: string(r.EntryID), EntryType: r.EntryType,
		ConversationID: r.ConversationID, LeadID: string(r.LeadID), BusinessPhoneID: r.BusinessPhoneID, ContactNumber: r.ContactNumber,
		MessageID: string(r.MessageID), TemplateID: string(r.TemplateID), WorkflowRunID: string(r.WorkflowRunID), SentBy: string(r.SentBy),
		Status: whatsappform.SendStatus(r.Status), FailureReason: r.FailureReason, CreatedAt: r.CreatedAt, SentAt: r.SentAt, RespondedAt: r.RespondedAt,
	}
}

func (s *SendStore) Insert(ctx context.Context, send whatsappform.Send) error {
	if strings.TrimSpace(send.Token) == "" {
		return whatsappform.ErrTokenRequired
	}
	if !database.AllUUIDs(send.ID, send.WorkspaceID, send.FormID, send.VersionID, send.PublicationID, send.BusinessPhoneID) {
		return errIDsInvalid
	}
	return s.db.WithContext(ctx).Exec(insertSendSQL, send.ID, send.WorkspaceID, send.FormID, send.VersionID, send.PublicationID, send.Token,
		string(send.Origin), send.OriginRef, schema.OptionalText(send.EntryID), send.EntryType, send.ConversationID, schema.OptionalText(send.LeadID),
		send.BusinessPhoneID, send.ContactNumber, schema.OptionalText(send.MessageID), schema.OptionalText(send.TemplateID),
		schema.OptionalText(send.WorkflowRunID), schema.OptionalText(send.SentBy), string(send.Status), send.FailureReason,
		send.CreatedAt, send.SentAt, send.RespondedAt).Error
}

func (s *SendStore) Update(ctx context.Context, send whatsappform.Send) error {
	if !database.AllUUIDs(send.ID, send.WorkspaceID) {
		return whatsappform.ErrSendNotFound
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(updateSendSQL, string(send.Status), schema.OptionalText(send.MessageID), send.FailureReason,
		send.SentAt, send.RespondedAt, send.WorkspaceID, send.ID), whatsappform.ErrSendNotFound)
}

func (s *SendStore) FindByToken(ctx context.Context, token string) (*whatsappform.Send, error) {
	if strings.TrimSpace(token) == "" {
		return nil, whatsappform.ErrSendNotFound
	}
	return s.one(ctx, findSendByTokenSQL, token)
}

func (s *SendStore) FindByMessageID(ctx context.Context, messageID string) (*whatsappform.Send, error) {
	if strings.TrimSpace(messageID) == "" {
		return nil, whatsappform.ErrSendNotFound
	}
	return s.one(ctx, findSendByMessageSQL, messageID)
}

func (s *SendStore) List(ctx context.Context, q whatsappform.SendQuery) (whatsappform.Page[whatsappform.Send], error) {
	empty := whatsappform.Page[whatsappform.Send]{Items: []whatsappform.Send{}}
	if !q.Scope.Resolved {
		return empty, whatsappform.ErrForbidden
	}
	limit, err := pageSize(q.Limit)
	if err != nil {
		return empty, err
	}
	cursor, err := cursorOf(q.After)
	if err != nil {
		return empty, err
	}
	if !database.AllUUIDs(q.WorkspaceID, q.FormID) {
		return empty, nil
	}
	sql, args, err := sendListQuery(q, cursor, limit)
	if err != nil {
		return empty, err
	}
	sends, err := s.sends(ctx, sql, args...)
	if err != nil {
		return empty, err
	}
	return pageOf(sends, limit, func(send whatsappform.Send) shared.Keyset {
		return shared.Keyset{At: send.CreatedAt, ID: send.ID}
	}), nil
}

func sendListQuery(q whatsappform.SendQuery, cursor *shared.Keyset, limit int) (string, []interface{}, error) {
	scope, scopeArgs, err := scopeClause(q.Scope)
	if err != nil {
		return "", nil, err
	}
	filter := where(sendsFromSQL+joinsFor("s")+" WHERE s.workspace_id = ? AND s.form_id = ?", q.WorkspaceID, q.FormID).and(scope, scopeArgs...)
	if cursor != nil {
		filter.and(" AND (s.created_at, s.id) < (?, ?::uuid)", cursor.At, cursor.ID)
	}
	filter.and(" ORDER BY s.created_at DESC, s.id DESC LIMIT ?", limit+1)
	return filter.String(), filter.args, nil
}

func (s *SendStore) one(ctx context.Context, sql string, args ...interface{}) (*whatsappform.Send, error) {
	found, err := s.sends(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, whatsappform.ErrSendNotFound
	}
	return &found[0], nil
}

func (s *SendStore) sends(ctx context.Context, sql string, args ...interface{}) ([]whatsappform.Send, error) {
	var rows []sendRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	sends := make([]whatsappform.Send, 0, len(rows))
	for _, row := range rows {
		sends = append(sends, row.toDomain())
	}
	return sends, nil
}
