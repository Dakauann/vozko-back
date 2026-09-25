package template

import (
	"errors"
	"fmt"
	"strings"
)

var ErrTemplateParamsMismatch = errors.New("whatsapp template send: every template variable needs exactly one value")

func (t *Template) EnsureSendable() error {
	if t.IsReadyToSend() {
		return nil
	}
	message := t.GetUsabilityMessage()
	if message == "" {
		message = fmt.Sprintf("template status is %s", t.Status)
	}
	return fmt.Errorf("%w: %s", ErrTemplateNotSendable, message)
}

func (t *Template) BelongsToWABA(wabaID string) bool {
	own := strings.TrimSpace(t.WABAId)
	return own == "" || strings.EqualFold(own, strings.TrimSpace(wabaID))
}

func (t *Template) ValidateParams(body, header []string) error {
	bodyNames, headerNames := t.GetBodyAndHeaderParameterNames()
	if !everySlotFilled(body, len(bodyNames)) || !everySlotFilled(header, len(headerNames)) {
		return ErrTemplateParamsMismatch
	}
	return nil
}

func everySlotFilled(values []string, slots int) bool {
	if len(values) != slots {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}
