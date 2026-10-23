package emailapi_usecase

import (
	"errors"

	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
	"vozko/domain/media"
	"vozko/usecases/emailinbox"
	tx "vozko/usecases/transactionalemail"
)

const (
	CodeIdempotencyKeyInvalid = "email_idempotency_key_invalid"
	CodeIdempotencyKeyReused  = "email_idempotency_key_reused"
	CodeFromRequired          = "email_from_required"
	CodeSenderUnknown         = "email_sender_unknown"
	CodeRecipientsInvalid     = "email_recipients_invalid"
	CodeReplyToInvalid        = "email_reply_to_invalid"
	CodeHeaderNotAllowed      = "email_header_not_allowed"
	CodeTagsInvalid           = "email_tags_invalid"
	CodeAttachmentInvalid     = "email_attachment_invalid"
	CodeAttachmentBlocked     = emailinbox.SendCodeAttachmentBlocked
	CodeAttachmentTooLarge    = emailinbox.SendCodeAttachmentTooLarge
	CodeAttachmentUnavailable = "email_attachment_unavailable"
	CodeMessageNotFound       = "email_message_not_found"
	CodeLogCursorInvalid      = "email_log_cursor_invalid"
	CodeLogFilterInvalid      = "email_log_filter_invalid"
)

var requestCodes = []struct {
	err  error
	code string
}{
	{emailapi.ErrWorkspaceRequired, tx.CodeRequestInvalid},
	{emailapi.ErrOriginInvalid, tx.CodeRequestInvalid},
	{emailapi.ErrIdempotencyKeyRequired, tx.CodeIdempotencyKeyRequired},
	{emailapi.ErrIdempotencyKeyInvalid, CodeIdempotencyKeyInvalid},
	{emailapi.ErrIdempotencyKeyReused, CodeIdempotencyKeyReused},
	{emailapi.ErrFromRequired, CodeFromRequired},
	{emailapi.ErrSenderUnknown, CodeSenderUnknown},
	{emailapi.ErrRecipientsInvalid, CodeRecipientsInvalid},
	{emailapi.ErrAddressInvalid, tx.CodeAddressInvalid},
	{emailapi.ErrReplyToInvalid, CodeReplyToInvalid},
	{emailapi.ErrSubjectInvalid, tx.CodeSubjectInvalid},
	{emailapi.ErrBodyInvalid, tx.CodeMessageInvalid},
	{emailapi.ErrHeaderNotAllowed, CodeHeaderNotAllowed},
	{emailapi.ErrTagsInvalid, CodeTagsInvalid},
	{emailapi.ErrAttachmentInvalid, CodeAttachmentInvalid},
	{emailapi.ErrSendInProgress, tx.CodeSendInProgress},
	{emailapi.ErrMessageNotFound, CodeMessageNotFound},
	{emailapi.ErrCursorInvalid, CodeLogCursorInvalid},
	{emailapi.ErrFilterInvalid, CodeLogFilterInvalid},
	{es.ErrAttachmentInvalid, CodeAttachmentInvalid},
	{es.ErrAttachmentBlocked, CodeAttachmentBlocked},
	{es.ErrAttachmentTooLarge, CodeAttachmentTooLarge},
	{es.ErrMessageTooLarge, CodeAttachmentTooLarge},
	{media.ErrMediaTooLarge, CodeAttachmentTooLarge},
	{es.ErrDomainNotVerified, tx.CodeSenderNotVerified},
}

func refusal(err error) error {
	var known *tx.Refusal
	if errors.As(err, &known) {
		return err
	}
	for _, entry := range requestCodes {
		if errors.Is(err, entry.err) {
			return tx.Refuse(entry.code, err)
		}
	}
	return err
}

func recipientScoped(code string) bool {
	return code == tx.CodeRecipientSuppressed || code == tx.CodeAddressInvalid
}
