package template_usecase

import (
	"fmt"

	"vozko/domain/whatsapp/template"
)

type TemplateHealthSignals interface {
	TemplateChanged(externalID string)
}

type HealthSignalling interface {
	SetHealthSignals(signals TemplateHealthSignals)
}

func (h *handleTemplateWebhook) SetHealthSignals(signals TemplateHealthSignals) {
	h.signals = signals
}

func (h *handleTemplateWebhook) signalChanged(recordErr error, v template.TemplateWebhookValue) {
	if recordErr != nil || h.signals == nil || v.MessageTemplateID == 0 {
		return
	}
	h.signals.TemplateChanged(fmt.Sprintf("%d", v.MessageTemplateID))
}
