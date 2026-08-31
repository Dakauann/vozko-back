package unofficial_whatsapp_campaign_repository

import (
	"encoding/json"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/database/schema"
)

// Translation between the domain and the row.
//
// The message spec round-trips through jsonb rather than being spread over
// columns: it is a closed value the domain validates as a whole, and columns
// would mean a migration every time a message kind gains a field.

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func encodeMessage(m uwc.MessageSpec) schema.LeadMetadata {
	blob, err := json.Marshal(m)
	if err != nil {
		return schema.LeadMetadata{}
	}
	var out schema.LeadMetadata
	if err := json.Unmarshal(blob, &out); err != nil {
		return schema.LeadMetadata{}
	}
	return out
}

func decodeMessage(raw schema.LeadMetadata) uwc.MessageSpec {
	var spec uwc.MessageSpec
	blob, err := json.Marshal(raw)
	if err != nil {
		return spec
	}
	_ = json.Unmarshal(blob, &spec)
	return spec
}

func toRow(c *uwc.Campaign) *schema.UnofficialWhatsAppCampaign {
	return &schema.UnofficialWhatsAppCampaign{
		ID:                   c.ID,
		WorkspaceID:          c.WorkspaceID,
		DepartmentID:         ptr(c.DepartmentID),
		InstanceID:           c.InstanceID,
		CreatedByID:          ptr(c.CreatedByID),
		Name:                 c.Name,
		MessageKind:          string(c.Message.Kind),
		Message:              encodeMessage(c.Message),
		AgentID:              ptr(c.AgentID),
		WorkflowID:           ptr(c.WorkflowID),
		PipelineID:           ptr(c.PipelineID),
		EnableAgentResponses: c.EnableAgentResponses,
		EnableWorkflow:       c.EnableWorkflow,
		EnableAnalysis:       c.EnableAnalysis,
		EnableAutoStaging:    c.EnableAutoStaging,
		EnableAutoMemory:     c.EnableAutoMemory,
		PreferAudio:          c.PreferAudio,
		AiModel:              c.AiModel,
		SendDelayMinMS:       c.SendDelayMinMS,
		SendDelayMaxMS:       c.SendDelayMaxMS,
		DailyCap:             c.DailyCap,
		Status:               string(c.Status),
		StatusReason:         c.StatusReason,
		ResetCode:            c.ResetCode,
		ClearCode:            c.ClearCode,
		ScheduledStart:       c.ScheduledStart,
		Archived:             c.Archived,
	}
}

func toDomain(r *schema.UnofficialWhatsAppCampaign) *uwc.Campaign {
	if r == nil {
		return nil
	}
	return &uwc.Campaign{
		ID:                   r.ID,
		WorkspaceID:          r.WorkspaceID,
		DepartmentID:         str(r.DepartmentID),
		InstanceID:           r.InstanceID,
		CreatedByID:          str(r.CreatedByID),
		Name:                 r.Name,
		Message:              decodeMessage(r.Message),
		AgentID:              str(r.AgentID),
		WorkflowID:           str(r.WorkflowID),
		PipelineID:           str(r.PipelineID),
		EnableAgentResponses: r.EnableAgentResponses,
		EnableWorkflow:       r.EnableWorkflow,
		EnableAnalysis:       r.EnableAnalysis,
		EnableAutoStaging:    r.EnableAutoStaging,
		EnableAutoMemory:     r.EnableAutoMemory,
		PreferAudio:          r.PreferAudio,
		AiModel:              r.AiModel,
		SendDelayMinMS:       r.SendDelayMinMS,
		SendDelayMaxMS:       r.SendDelayMaxMS,
		DailyCap:             r.DailyCap,
		Status:               campaign.Status(r.Status),
		StatusReason:         r.StatusReason,
		ResetCode:            r.ResetCode,
		ClearCode:            r.ClearCode,
		ScheduledStart:       r.ScheduledStart,
		Archived:             r.Archived,
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
	}
}

func entryToRow(e uwc.Entry) schema.UnofficialWhatsAppCampaignEntry {
	meta := schema.LeadMetadata{}
	for k, v := range e.Metadata {
		meta[k] = v
	}
	return schema.UnofficialWhatsAppCampaignEntry{
		ID:                e.ID,
		CampaignID:        e.CampaignID,
		WorkspaceID:       e.WorkspaceID,
		LeadID:            e.LeadID,
		Number:            e.Number,
		Name:              e.Name,
		ContactID:         ptr(e.ContactID),
		ConversationID:    ptr(e.ConversationID),
		JID:               e.JID,
		CheckedAt:         e.CheckedAt,
		Status:            string(e.Status),
		VariantIndex:      e.VariantIndex,
		ProviderMessageID: e.ProviderMessageID,
		MessageID:         ptr(e.MessageID),
		ErrorCode:         e.ErrorCode,
		ErrorMessage:      e.ErrorMessage,
		Variables:         e.Variables,
		Metadata:          meta,
		SentAt:            e.SentAt,
	}
}

func entryToDomain(r *schema.UnofficialWhatsAppCampaignEntry) *uwc.Entry {
	if r == nil {
		return nil
	}
	meta := map[string]interface{}{}
	for k, v := range r.Metadata {
		meta[k] = v
	}
	return &uwc.Entry{
		ID:                r.ID,
		CampaignID:        r.CampaignID,
		WorkspaceID:       r.WorkspaceID,
		LeadID:            r.LeadID,
		Number:            r.Number,
		Name:              r.Name,
		ContactID:         str(r.ContactID),
		ConversationID:    str(r.ConversationID),
		JID:               r.JID,
		CheckedAt:         r.CheckedAt,
		Status:            campaign.SendStatus(r.Status),
		VariantIndex:      r.VariantIndex,
		ProviderMessageID: r.ProviderMessageID,
		MessageID:         str(r.MessageID),
		ErrorCode:         r.ErrorCode,
		ErrorMessage:      r.ErrorMessage,
		Variables:         r.Variables,
		Metadata:          meta,
		SentAt:            r.SentAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}
