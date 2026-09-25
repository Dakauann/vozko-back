package scheduled_message_usecase

import (
	sm "vozko/domain/scheduled_message"
	wo "vozko/domain/whatsapp_outreach"
)

const templateIdempotencyPrefix = "scheduled-message:"

func templateSend(m *sm.ScheduledMessage) wo.ConversationTemplateInput {
	return wo.ConversationTemplateInput{
		WorkspaceID:    m.WorkspaceID,
		UserID:         m.CreatedByUserID,
		EntryID:        m.EntryID,
		TemplateID:     m.Template.ID,
		BodyParams:     m.Template.BodyParams,
		HeaderParams:   m.Template.HeaderParams,
		IdempotencyKey: templateIdempotencyPrefix + m.ID,
	}
}
