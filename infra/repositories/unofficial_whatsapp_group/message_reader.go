package unofficial_whatsapp_group_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

type messageReader struct{ db *gorm.DB }

func NewMessageReader(db *gorm.DB) uwg.MessageReader { return &messageReader{db: db} }

func (r *messageReader) GroupConversation(ctx context.Context, instanceID, groupJID string) (string, error) {
	var id string
	err := r.db.WithContext(ctx).Raw(`SELECT id FROM unofficial_whatsapp_conversations
		WHERE instance_id = ? AND chat_id = ? AND campaign_id = '' AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, instanceID, groupJID).Scan(&id).Error
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", uwg.ErrNoGroupConversation
	}
	return id, nil
}

type transcriptRow struct {
	ID              string
	CreatedAt       time.Time
	FromParticipant string
	Text            string
	Direction       string
}

const transcriptColumns = `SELECT id, created_at, from_participant, text, direction
	FROM conversation_messages
	WHERE entry_id = ? AND entry_type = 'unofficial_whatsapp' AND deleted_at IS NULL AND retracted_at IS NULL
	  AND message_type <> 'reaction' AND btrim(text) <> '' AND text NOT LIKE '[%]'`

func (r *messageReader) TextAfter(ctx context.Context, conversationID string, after uwg.Cursor, limit int) ([]uwg.TranscriptLine, error) {
	var rows []transcriptRow
	err := r.db.WithContext(ctx).Raw(transcriptColumns+`
		AND (created_at, id) > (?, ?) ORDER BY created_at ASC, id ASC LIMIT ?`, conversationID, after.At, after.ID, limit).Scan(&rows).Error
	return transcript(rows), err
}

func (r *messageReader) Between(ctx context.Context, conversationID string, from, to time.Time, offset, limit int) ([]uwg.TranscriptLine, error) {
	var rows []transcriptRow
	err := r.db.WithContext(ctx).Raw(transcriptColumns+`
		AND created_at >= ? AND created_at <= ? ORDER BY created_at ASC, id ASC OFFSET ? LIMIT ?`,
		conversationID, from, to, offset, limit).Scan(&rows).Error
	return transcript(rows), err
}

func transcript(rows []transcriptRow) []uwg.TranscriptLine {
	out := make([]uwg.TranscriptLine, 0, len(rows))
	for _, rw := range rows {
		out = append(out, uwg.TranscriptLine{
			ID: rw.ID, At: rw.CreatedAt, Author: rw.FromParticipant, Text: rw.Text,
			Outbound: rw.Direction == string(conversation.MessageDirectionOutbound),
		})
	}
	return out
}
