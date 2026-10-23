package workflow

import (
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
)

var permanentSendRefusals = []error{
	conversation.ErrWindowClosed,
	conversation.ErrOutboundWindowClosed,
	conversation.ErrRecipientUnreachable,
	conversation.ErrMessageTypeUnsupported,
	wo.ErrLeadBlocked,
	wo.ErrLeadOptedOut,
	wo.ErrInvalidPhone,
	template.ErrTemplateNotSendable,
}

func IsPermanentRefusal(err error) bool {
	if err == nil {
		return false
	}
	if ClassifyError(err) == ErrorKindWindowClosed {
		return true
	}
	for _, refusal := range permanentSendRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
