package conversation

import "errors"

var (
	ErrEntryIDRequired            = errors.New("conversation: entry id is required")
	ErrEntryTypeInvalid           = errors.New("conversation: entry type is invalid")
	ErrMessageIDRequired          = errors.New("conversation: message id is required")
	ErrMessageNotFound            = errors.New("conversation: message not found")
	ErrMessageContentRequired     = errors.New("conversation: message content is required")
	ErrMessageParticipantRequired = errors.New("conversation: at least one participant is required")
	ErrMessageSenderRequired      = errors.New("conversation: a message must say who sent it")
	ErrWhatsAppWebhookSkipped     = errors.New("conversation: whatsapp webhook skipped")
	ErrWhatsAppWebhookRetryable   = errors.New("conversation: whatsapp webhook retryable failure")
	ErrRefundNotConfigured        = errors.New("conversation: whatsapp refund dependencies are required")
	ErrMediaRequired              = errors.New("conversation: media is required")
	ErrMediaTypeInvalid           = errors.New("conversation: media type is invalid")
	ErrMediaURLRequired           = errors.New("conversation: media url is required")
	ErrMediaNotFound              = errors.New("conversation: media not found")
	ErrConversationNotFound       = errors.New("conversation: conversation not found")
	ErrUnauthorized               = errors.New("conversation: unauthorized access")
	ErrWindowClosed               = errors.New("conversation: 24-hour messaging window is closed, cannot send message")
	ErrTemplateNotGranted         = errors.New("conversation: the workspace has no access to this template")

	ErrWhatsAppCallNoPermission  = errors.New("conversation: whatsapp call permission not granted by user (138006)")
	ErrWhatsAppCallNotConfigured = errors.New("conversation: whatsapp calling not configured")
	ErrNoCallSource              = errors.New("conversation: no call source configured")

	ErrConversationIDRequired = ErrEntryIDRequired
)
