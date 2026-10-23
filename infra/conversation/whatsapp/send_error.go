package whatsapp

import (
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/metaerror"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/infra/meta"
)

var permanentSendRefusals = map[int]error{
	metaerror.ReEngagement:     conversation.ErrWindowClosed,
	metaerror.Undeliverable:    wo.ErrInvalidPhone,
	metaerror.MarketingOptOut:  wo.ErrLeadOptedOut,
	metaerror.UnsupportedType:  conversation.ErrMessageTypeUnsupported,
	metaerror.TemplateMissing:  template.ErrTemplateNotSendable,
	metaerror.TemplatePaused:   template.ErrTemplateNotSendable,
	metaerror.TemplateDisabled: template.ErrTemplateNotSendable,
}

type sendAPIError struct {
	message string
	Meta    *meta.Error
	refusal error
}

func (e *sendAPIError) Error() string { return e.message }

func (e *sendAPIError) Unwrap() []error {
	wrapped := []error{e.Meta}
	if e.refusal != nil {
		wrapped = append(wrapped, e.refusal)
	}
	return wrapped
}

func newSendAPIError(message string, status int, body []byte) *sendAPIError {
	parsed := parseMetaError(status, body)
	return &sendAPIError{message: message, Meta: parsed, refusal: permanentSendRefusals[parsed.Code]}
}
