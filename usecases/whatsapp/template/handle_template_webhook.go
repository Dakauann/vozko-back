package template_usecase

import (
	"fmt"
	"log"
	"strings"

	"vozko/domain/whatsapp/template"
)

type handleTemplateWebhook struct {
	repo template.Repository
}

func NewHandleTemplateWebhook(repo template.Repository) template.HandleTemplateWebhookUseCase {
	return &handleTemplateWebhook{repo: repo}
}

func (h *handleTemplateWebhook) Execute(payload *template.TemplateWebhookPayload) error {
	if payload == nil {
		return nil
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			switch change.Field {
			case template.FieldMessageTemplateStatusUpdate:
				h.handleStatusUpdate(entry.ID, change.Value)
			case template.FieldMessageTemplateQualityUpdate:
				h.handleQualityUpdate(entry.ID, change.Value)
			case template.FieldMessageTemplateComponentsUpdate:
				h.handleComponentsUpdate(entry.ID, change.Value)
			case template.FieldTemplateCategoryUpdate:
				h.handleCategoryUpdate(entry.ID, change.Value)
			default:
				log.Printf("[template-webhook] unhandled field: %s", change.Field)
			}
		}
	}
	return nil
}

func eventToStatus(event string) (template.TemplateStatus, bool) {
	switch strings.ToUpper(event) {
	case "APPROVED", "REINSTATED":
		return template.TemplateStatusApproved, true
	case "REJECTED":
		return template.TemplateStatusRejected, true
	case "PAUSED", "LOCKED":
		return template.TemplateStatusPaused, true
	case "DISABLED":
		return template.TemplateStatusDisabled, true
	case "PENDING", "IN_APPEAL":
		return template.TemplateStatusPending, true
	default:
		return "", false
	}
}

func (h *handleTemplateWebhook) handleStatusUpdate(wabaID string, v template.TemplateWebhookValue) {
	// 360dialog identifies the template by its channel-scoped id (what we store as
	// ExternalID); Meta uses the numeric message_template_id. Prefer the string id
	// when present so both webhook sources resolve the same stored record.
	externalID := strings.TrimSpace(v.ChannelExternalID)
	if externalID == "" && v.MessageTemplateID != 0 {
		externalID = fmt.Sprintf("%d", v.MessageTemplateID)
	}
	if externalID == "" {
		log.Printf("[template-webhook] status_update: missing template id")
		return
	}

	event := strings.ToUpper(v.Event)

	if event == "DELETED" || event == "PENDING_DELETION" {
		tmpl := h.findTemplate(externalID, wabaID)
		if tmpl == nil {
			return
		}
		if err := h.repo.Delete(tmpl.ID); err != nil {
			log.Printf("[template-webhook] status_update: failed to delete template %s: %v", externalID, err)
		} else {
			log.Printf("[template-webhook] status_update: template %s (%s) deleted", v.MessageTemplateName, externalID)
		}
		return
	}

	newStatus, ok := eventToStatus(event)
	if !ok {
		log.Printf("[template-webhook] status_update: unrecognised event %q for template %s", event, externalID)
		return
	}

	tmpl := h.findTemplate(externalID, wabaID)
	if tmpl == nil {
		return
	}

	changed := false

	if tmpl.Status != newStatus {
		tmpl.Status = newStatus
		changed = true
	}

	if v.MessageTemplateCategory != "" {
		cat := template.TemplateCategory(strings.ToUpper(v.MessageTemplateCategory))
		if cat.IsValid() && tmpl.Category != cat {
			tmpl.Category = cat
			changed = true
		}
	}

	if changed {
		if err := h.repo.Update(tmpl.ID, tmpl); err != nil {
			log.Printf("[template-webhook] status_update: failed to update template %s: %v", externalID, err)
		} else {
			log.Printf("[template-webhook] status_update: template %s (%s) → %s", v.MessageTemplateName, externalID, newStatus)
		}
	}
}

func (h *handleTemplateWebhook) handleQualityUpdate(wabaID string, v template.TemplateWebhookValue) {
	externalID := fmt.Sprintf("%d", v.MessageTemplateID)
	if v.MessageTemplateID == 0 {
		log.Printf("[template-webhook] quality_update: missing template id")
		return
	}

	log.Printf("[template-webhook] quality_update: template %s (%s) quality %s → %s",
		v.MessageTemplateName, externalID, v.PreviousQualityScore, v.NewQualityScore)

}

func (h *handleTemplateWebhook) handleComponentsUpdate(wabaID string, v template.TemplateWebhookValue) {
	externalID := fmt.Sprintf("%d", v.MessageTemplateID)
	if v.MessageTemplateID == 0 {
		log.Printf("[template-webhook] components_update: missing template id")
		return
	}
	log.Printf("[template-webhook] components_update: template %s (%s) components changed",
		v.MessageTemplateName, externalID)
}

func (h *handleTemplateWebhook) handleCategoryUpdate(wabaID string, v template.TemplateWebhookValue) {
	externalID := fmt.Sprintf("%d", v.MessageTemplateID)
	if v.MessageTemplateID == 0 {
		log.Printf("[template-webhook] category_update: missing template id")
		return
	}

	if v.NewCategory == "" {
		log.Printf("[template-webhook] category_update: missing new_category for template %s", externalID)
		return
	}

	newCat := template.TemplateCategory(strings.ToUpper(v.NewCategory))
	if !newCat.IsValid() {
		log.Printf("[template-webhook] category_update: invalid category %q for template %s", v.NewCategory, externalID)
		return
	}

	tmpl := h.findTemplate(externalID, wabaID)
	if tmpl == nil {
		return
	}

	if tmpl.Category != newCat {
		tmpl.Category = newCat
		if err := h.repo.Update(tmpl.ID, tmpl); err != nil {
			log.Printf("[template-webhook] category_update: failed to update template %s: %v", externalID, err)
		} else {
			log.Printf("[template-webhook] category_update: template %s (%s) → %s", v.MessageTemplateName, externalID, newCat)
		}
	}
}

func (h *handleTemplateWebhook) findTemplate(externalID, wabaID string) *template.Template {
	if wabaID != "" {
		tmpl, err := h.repo.FindByExternalIDAndWABA(externalID, wabaID)
		if err == nil {
			return tmpl
		}
	}
	tmpl, err := h.repo.FindByExternalID(externalID)
	if err != nil {
		log.Printf("[template-webhook] template %s not found: %v", externalID, err)
		return nil
	}
	return tmpl
}
