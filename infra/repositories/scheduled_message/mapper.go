package scheduled_message_repository

import (
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func toDomain(row *schema.ScheduledMessage) *sm.ScheduledMessage {
	if row == nil {
		return nil
	}

	m := &sm.ScheduledMessage{
		ID:                        row.ID,
		WorkspaceID:               row.WorkspaceID,
		EntryID:                   row.EntryID,
		EntryType:                 shared.EntryType(row.EntryType),
		CreatedByUserID:           row.CreatedByUserID,
		Text:                      row.Text,
		MediaID:                   row.MediaID,
		MediaType:                 row.MediaType,
		ReplyToMessageID:          row.ReplyToMessageID,
		Signed:                    row.Signed,
		ScheduledAt:               row.ScheduledAt,
		WindowExpiresAtAtCreation: row.WindowExpiresAtAtCreation,
		Status:                    sm.Status(row.Status),
		FailureDetail:             row.FailureDetail,
		ClaimedAt:                 row.ClaimedAt,
		SentAt:                    row.SentAt,
		SentMessageID:             row.SentMessageID,
		IdempotencyKey:            row.IdempotencyKey,
		CreatedAt:                 row.CreatedAt,
		UpdatedAt:                 row.UpdatedAt,
	}
	if row.FailureReason != nil {
		reason := sm.FailureReason(*row.FailureReason)
		m.FailureReason = &reason
	}
	return m
}

func toDomainSlice(rows []schema.ScheduledMessage) []*sm.ScheduledMessage {
	out := make([]*sm.ScheduledMessage, len(rows))
	for i := range rows {
		out[i] = toDomain(&rows[i])
	}
	return out
}

func fromDomain(m *sm.ScheduledMessage) schema.ScheduledMessage {
	row := schema.ScheduledMessage{
		ID:                        m.ID,
		WorkspaceID:               m.WorkspaceID,
		EntryID:                   m.EntryID,
		EntryType:                 string(m.EntryType),
		CreatedByUserID:           m.CreatedByUserID,
		Text:                      m.Text,
		MediaID:                   m.MediaID,
		MediaType:                 m.MediaType,
		ReplyToMessageID:          m.ReplyToMessageID,
		Signed:                    m.Signed,
		ScheduledAt:               m.ScheduledAt,
		WindowExpiresAtAtCreation: m.WindowExpiresAtAtCreation,
		Status:                    string(m.Status),
		FailureDetail:             m.FailureDetail,
		ClaimedAt:                 m.ClaimedAt,
		SentAt:                    m.SentAt,
		SentMessageID:             m.SentMessageID,
		IdempotencyKey:            m.IdempotencyKey,
	}
	if m.FailureReason != nil {
		reason := string(*m.FailureReason)
		row.FailureReason = &reason
	}
	return row
}

func statusStrings(statuses []sm.Status) []string {
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	return out
}
