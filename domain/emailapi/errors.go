package emailapi

import "errors"

var (
	ErrWorkspaceRequired      = errors.New("emailapi: the workspace is required")
	ErrOriginInvalid          = errors.New("emailapi: unknown origin")
	ErrIdempotencyKeyRequired = errors.New("emailapi: idempotency_key is required")
	ErrIdempotencyKeyInvalid  = errors.New("emailapi: idempotency_key needs 1 to 256 printable characters")
	ErrIdempotencyKeyReused   = errors.New("emailapi: idempotency_key was used for another request")
	ErrFromRequired           = errors.New("emailapi: from is required")
	ErrSenderUnknown          = errors.New("emailapi: from is not a sender of the workspace")
	ErrRecipientsInvalid      = errors.New("emailapi: send to 1 to 50 recipients")
	ErrAddressInvalid         = errors.New("emailapi: a recipient address is invalid")
	ErrReplyToInvalid         = errors.New("emailapi: reply_to takes up to 5 valid addresses")
	ErrSubjectInvalid         = errors.New("emailapi: the subject needs 1 to 998 characters without line breaks")
	ErrBodyInvalid            = errors.New("emailapi: send html and/or text, or design_id with variables")
	ErrHeaderNotAllowed       = errors.New("emailapi: header not allowed")
	ErrTagsInvalid            = errors.New("emailapi: tags take up to 10 distinct names of letters, digits, _ and -")
	ErrAttachmentInvalid      = errors.New("emailapi: an attachment needs a file name and either content or an https path")
	ErrSendInProgress         = errors.New("emailapi: another call is processing this send")
	ErrMessageNotFound        = errors.New("emailapi: message not found")
	ErrCursorInvalid          = errors.New("emailapi: invalid cursor")
	ErrFilterInvalid          = errors.New("emailapi: invalid filter")
)
