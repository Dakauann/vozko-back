package scheduled_message_repository

import (
	"encoding/json"
	"fmt"
	"log"

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
		Kind:                      sm.Kind(row.Kind),
		Template:                  templateFromRow(row),
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

func fromDomain(m *sm.ScheduledMessage) (schema.ScheduledMessage, error) {
	row := schema.ScheduledMessage{
		ID:                        m.ID,
		WorkspaceID:               m.WorkspaceID,
		EntryID:                   m.EntryID,
		EntryType:                 string(m.EntryType),
		CreatedByUserID:           m.CreatedByUserID,
		Kind:                      string(m.Kind),
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
	if err := templateToRow(m.Template, &row); err != nil {
		return schema.ScheduledMessage{}, err
	}
	return row, nil
}

func templateToRow(t *sm.TemplateContent, row *schema.ScheduledMessage) error {
	if t == nil {
		return nil
	}
	body, err := json.Marshal(t.BodyParams)
	if err != nil {
		return fmt.Errorf("scheduled message: encode template body values: %w", err)
	}
	header, err := json.Marshal(t.HeaderParams)
	if err != nil {
		return fmt.Errorf("scheduled message: encode template header values: %w", err)
	}
	id := t.ID
	row.TemplateID = &id
	row.TemplateName = t.Name
	row.TemplatePreview = t.Preview
	row.TemplateBodyParams = body
	row.TemplateHeaderParams = header
	return nil
}

func templateFromRow(row *schema.ScheduledMessage) *sm.TemplateContent {
	if row.TemplateID == nil {
		return nil
	}
	return &sm.TemplateContent{
		ID:           *row.TemplateID,
		Name:         row.TemplateName,
		Preview:      row.TemplatePreview,
		BodyParams:   decodeValues(row.ID, "body", row.TemplateBodyParams),
		HeaderParams: decodeValues(row.ID, "header", row.TemplateHeaderParams),
	}
}

func decodeValues(id, part string, raw []byte) []string {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		log.Printf("[scheduled_message] %s has unreadable template %s values, the send rules will refuse it: %v", id, part, err)
		return nil
	}
	return values
}

func statusStrings(statuses []sm.Status) []string {
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	return out
}
