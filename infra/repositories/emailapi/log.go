package emailapi_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/emailapi"
	"vozko/infra/database/schema"
)

type Log struct {
	db *gorm.DB
}

var _ emailapi.LogReader = (*Log)(nil)

func NewLog(db *gorm.DB) *Log {
	return &Log{db: db}
}

const logColumns = `m.id, m.send_id, m.position, m.workspace_id, m.role, m.address, m.status, m.error_code, m.message_id, m.round,
	m.last_event, m.last_event_at, m.created_at, m.updated_at,
	s.idempotency_key AS send_idempotency_key, s.fingerprint AS send_fingerprint, s.origin AS send_origin, s.api_key_id AS send_api_key_id,
	s.sender_id AS send_sender_id, s.from_address AS send_from_address, s.subject AS send_subject, s.tags AS send_tags,
	s.recipients AS send_recipients, s.created_at AS send_created_at`

type logRow struct {
	schema.EmailAPIMessage
	SendIdempotencyKey string
	SendFingerprint    string
	SendOrigin         string
	SendAPIKeyID       schema.OptionalText
	SendSenderID       string
	SendFromAddress    string
	SendSubject        string
	SendTags           datatypes.JSON
	SendRecipients     int
	SendCreatedAt      time.Time
}

func (r logRow) entry() (emailapi.LogEntry, error) {
	send, err := sendOf(schema.EmailAPISend{
		ID: r.SendID, WorkspaceID: r.WorkspaceID, IdempotencyKey: r.SendIdempotencyKey, Fingerprint: r.SendFingerprint, Origin: r.SendOrigin,
		APIKeyID: r.SendAPIKeyID, SenderID: r.SendSenderID, FromAddress: r.SendFromAddress, Subject: r.SendSubject, Tags: r.SendTags,
		Recipients: r.SendRecipients, RoundSettled: true, CreatedAt: r.SendCreatedAt,
	})
	if err != nil {
		return emailapi.LogEntry{}, err
	}
	return emailapi.LogEntry{Message: messageOf(r.EmailAPIMessage), Send: send}, nil
}

func ListSQL(q emailapi.LogQuery) (string, []any) {
	var b strings.Builder
	var args []any
	keyCreated, keyID := "m.created_at", "m.id"
	if q.TagName != "" {
		keyCreated, keyID = "t.created_at", "t.message_row_id"
		b.WriteString("SELECT " + logColumns + " FROM email_api_message_tags t JOIN email_api_messages m ON m.id = t.message_row_id JOIN email_api_sends s ON s.id = m.send_id")
		b.WriteString(" WHERE t.workspace_id = ? AND t.name = ? AND t.value = ?")
		args = append(args, q.WorkspaceID, q.TagName, q.TagValue)
		if q.Status != "" {
			b.WriteString(" AND m.status = ?")
			args = append(args, string(q.Status))
		}
		if q.Recipient != "" {
			b.WriteString(" AND m.address = ?")
			args = append(args, string(q.Recipient))
		}
	} else {
		b.WriteString("SELECT " + logColumns + " FROM email_api_messages m JOIN email_api_sends s ON s.id = m.send_id WHERE m.workspace_id = ?")
		args = append(args, q.WorkspaceID)
		if q.Status != "" {
			b.WriteString(" AND m.status = ?")
			args = append(args, string(q.Status))
		}
		if q.Recipient != "" {
			b.WriteString(" AND m.address = ?")
			args = append(args, string(q.Recipient))
		}
	}
	if !q.From.IsZero() {
		b.WriteString(" AND " + keyCreated + " >= ?")
		args = append(args, q.From)
	}
	if !q.To.IsZero() {
		b.WriteString(" AND " + keyCreated + " < ?")
		args = append(args, q.To)
	}
	if !q.Cursor.Empty() {
		b.WriteString(" AND (" + keyCreated + ", " + keyID + ") < (?, ?)")
		args = append(args, q.Cursor.CreatedAt, q.Cursor.ID)
	}
	b.WriteString(" ORDER BY " + keyCreated + " DESC, " + keyID + " DESC LIMIT ?")
	args = append(args, q.Limit+1)
	return b.String(), args
}

func (l *Log) List(ctx context.Context, q emailapi.LogQuery) ([]emailapi.LogEntry, error) {
	sql, args := ListSQL(q)
	var rows []logRow
	if err := l.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return entries(rows)
}

func entries(rows []logRow) ([]emailapi.LogEntry, error) {
	out := make([]emailapi.LogEntry, 0, len(rows))
	for _, r := range rows {
		e, err := r.entry()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (l *Log) one(ctx context.Context, where string, args ...any) (*emailapi.LogEntry, error) {
	var rows []logRow
	sql := "SELECT " + logColumns + " FROM email_api_messages m JOIN email_api_sends s ON s.id = m.send_id WHERE " + where + " LIMIT 1"
	if err := l.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, emailapi.ErrMessageNotFound
	}
	e, err := rows[0].entry()
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (l *Log) Get(ctx context.Context, workspaceID, id string) (*emailapi.LogEntry, error) {
	return l.one(ctx, "m.workspace_id = ? AND m.id = ?", workspaceID, id)
}

func (l *Log) GetByMessageID(ctx context.Context, messageID string) (*emailapi.LogEntry, error) {
	return l.one(ctx, "m.message_id = ?", messageID)
}

func (l *Log) Events(ctx context.Context, messageID string) ([]emailapi.EventView, error) {
	var rows []schema.EmailMessageEvent
	if err := l.db.WithContext(ctx).Where("message_id = ?", messageID).Order("occurred_at, created_at").Limit(500).Find(&rows).Error; err != nil {
		return nil, notFound(err)
	}
	out := make([]emailapi.EventView, len(rows))
	for i, r := range rows {
		out[i] = emailapi.EventView{Kind: r.Kind, OccurredAt: r.OccurredAt, Link: r.Link, IsBot: r.IsBot, Detail: r.Detail}
	}
	return out, nil
}
