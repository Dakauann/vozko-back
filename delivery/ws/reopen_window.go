package ws

import wo "vozko/domain/whatsapp_outreach"

const (
	reopenWindowFailedCode            = "template_send_failed"
	reopenWindowFailedMessage         = "Failed to send template"
	reopenWindowOutcomeUnknownMessage = "WhatsApp may have delivered the template; check the conversation before sending again"
)

func reopenWindowRefusal(requestID, entryID, entryType string, err error) ErrorPayload {
	code := wo.ErrorCode(err)
	if code == "" {
		code = reopenWindowFailedCode
	}
	message := reopenWindowFailedMessage
	if code == wo.CodeSendOutcomeUnknown {
		message = reopenWindowOutcomeUnknownMessage
	}
	return ErrorPayload{Code: code, Message: message, RequestID: requestID, EntryID: entryID, EntryType: entryType}
}
