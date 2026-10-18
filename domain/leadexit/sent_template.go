package leadexit

import (
	"encoding/json"
	"strings"

	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
	"vozko/domain/lead"
)

type SentTemplate struct {
	MessageID       string
	WorkspaceID     string
	RecipientNumber string
	TemplateID      string
	Template        buttonaction.TemplateRef
	CampaignID      string
}

func (s SentTemplate) SentTo(workspaceID, number string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	return workspaceID != "" && strings.TrimSpace(s.WorkspaceID) == workspaceID && lead.SameNumber(s.RecipientNumber, number)
}

type storedTemplateInfo struct {
	TemplateName string `json:"template_name"`
	Language     string `json:"language"`
}

func TemplateOfMessage(m *conversation.Message) (SentTemplate, bool) {
	if m == nil || m.MessageType != conversation.MessageTypeTemplate || len(m.Metadata) == 0 {
		return SentTemplate{}, false
	}
	var info storedTemplateInfo
	if err := json.Unmarshal(m.Metadata, &info); err != nil {
		return SentTemplate{}, false
	}
	ref := buttonaction.NewTemplateRef(info.TemplateName, info.Language)
	if ref.Empty() {
		return SentTemplate{}, false
	}
	sent := SentTemplate{Template: ref, RecipientNumber: strings.TrimSpace(m.To)}
	if m.WhatsAppMessageID != nil {
		sent.MessageID = strings.TrimSpace(*m.WhatsAppMessageID)
	}
	if m.SentBy.Kind() == conversation.SenderCampaign {
		sent.CampaignID = m.SentBy.Participant()
	}
	return sent, true
}

func LastTemplateIn(history []*conversation.Message) (SentTemplate, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		if sent, ok := TemplateOfMessage(history[i]); ok {
			return sent, true
		}
	}
	return SentTemplate{}, false
}

func (s SentTemplate) knowsItsTemplate() bool {
	return strings.TrimSpace(s.TemplateID) != "" || !s.Template.Empty()
}

func (s SentTemplate) sameTemplate(other SentTemplate) bool {
	if s.TemplateID != "" && s.TemplateID == other.TemplateID {
		return true
	}
	return !s.Template.Empty() && s.Template == other.Template
}

func TemplatesInOrder(replied, last *SentTemplate) []SentTemplate {
	out := make([]SentTemplate, 0, 2)
	for _, sent := range []*SentTemplate{replied, last} {
		if sent == nil || !sent.knowsItsTemplate() || listed(out, *sent) {
			continue
		}
		out = append(out, *sent)
	}
	return out
}

func listed(sent []SentTemplate, candidate SentTemplate) bool {
	for _, known := range sent {
		if known.sameTemplate(candidate) {
			return true
		}
	}
	return false
}
