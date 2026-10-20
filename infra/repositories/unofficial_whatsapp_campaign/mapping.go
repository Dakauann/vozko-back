package unofficial_whatsapp_campaign_repository

import (
	"encoding/json"

	"github.com/lib/pq"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/database/schema"
)

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
		InstanceID:           schema.OptionalText(c.InstanceID),
		SenderInstanceIDs:    pq.StringArray(c.SenderInstanceIDs),
		CreatedByID:          ptr(c.CreatedByID),
		Source:               schema.OptionalText(c.Source),
		IdempotencyKey:       schema.OptionalText(c.IdempotencyKey),
		Name:                 c.Name,
		Audience:             string(c.Audience),
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
		InstanceID:           string(r.InstanceID),
		SenderInstanceIDs:    []string(r.SenderInstanceIDs),
		CreatedByID:          str(r.CreatedByID),
		Source:               string(r.Source),
		IdempotencyKey:       string(r.IdempotencyKey),
		Name:                 r.Name,
		Audience:             uwc.Audience(r.Audience),
		Message:              decodeMessage(r.Message),
		AgentID:              str(r.AgentID),
		WorkflowID:           str(r.WorkflowID),
		PipelineID:           str(r.PipelineID),
		EnableAgentResponses: r.EnableAgentResponses,
		EnableWorkflow:       r.EnableWorkflow,
		EnableAnalysis:       r.EnableAnalysis,
		EnableAutoStaging:    r.EnableAutoStaging,
		EnableAutoMemory:     r.EnableAutoMemory,
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
		LeadID:            ptr(e.LeadID),
		Number:            e.Number,
		Name:              e.Name,
		ContactID:         ptr(e.ContactID),
		ConversationID:    ptr(e.ConversationID),
		JID:               e.JID,
		CheckedAt:         e.CheckedAt,
		SenderInstanceID:  ptr(e.SenderInstanceID),
		Status:            string(e.Status),
		VariantIndex:      e.VariantIndex,
		ProviderMessageID: e.ProviderMessageID,
		MessageID:         ptr(e.MessageID),
		ExitMessageID:     ptr(e.ExitMessageID),
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
		LeadID:            str(r.LeadID),
		Number:            r.Number,
		Name:              r.Name,
		ContactID:         str(r.ContactID),
		ConversationID:    str(r.ConversationID),
		JID:               r.JID,
		CheckedAt:         r.CheckedAt,
		SenderInstanceID:  str(r.SenderInstanceID),
		Status:            campaign.SendStatus(r.Status),
		VariantIndex:      r.VariantIndex,
		ProviderMessageID: r.ProviderMessageID,
		MessageID:         str(r.MessageID),
		ExitMessageID:     str(r.ExitMessageID),
		ErrorCode:         r.ErrorCode,
		ErrorMessage:      r.ErrorMessage,
		Variables:         r.Variables,
		Metadata:          meta,
		SentAt:            r.SentAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}
