package conversation

import "errors"

var (
	ErrEntryIDRequired            = errors.New("conversation: entry id is required")
	ErrEntryTypeInvalid           = errors.New("conversation: entry type is invalid")
	ErrMessageIDRequired          = errors.New("conversation: message id is required")
	ErrMessageNotFound            = errors.New("conversation: message not found")
	ErrMessageContentRequired     = errors.New("conversation: message content is required")
	ErrMessageParticipantRequired = errors.New("conversation: at least one participant is required")
	ErrWhatsAppWebhookSkipped     = errors.New("conversation: whatsapp webhook skipped")
	ErrWhatsAppWebhookRetryable   = errors.New("conversation: whatsapp webhook retryable failure")
	// ErrRefundNotConfigured is the WhatsApp webhook handler refusing to exist
	// without what it needs to refund a failed template send. A refund that
	// never happens keeps money the customer paid for a message that never
	// arrived, and the old behaviour was to return nil and say nothing.
	ErrRefundNotConfigured  = errors.New("conversation: whatsapp refund dependencies are required")
	ErrMediaRequired        = errors.New("conversation: media is required")
	ErrMediaTypeInvalid     = errors.New("conversation: media type is invalid")
	ErrMediaURLRequired     = errors.New("conversation: media url is required")
	ErrMediaNotFound        = errors.New("conversation: media not found")
	ErrConversationNotFound = errors.New("conversation: conversation not found")
	ErrUnauthorized         = errors.New("conversation: unauthorized access")
	ErrWindowClosed         = errors.New("conversation: 24-hour messaging window is closed, cannot send message")

	ErrWhatsAppCallNoPermission  = errors.New("conversation: whatsapp call permission not granted by user (138006)")
	ErrWhatsAppCallNotConfigured = errors.New("conversation: whatsapp calling not configured")
	// ErrNoCallSource: the dial reached the dispatcher with no channel call
	// source wired at all, so there is nothing to place the call on.
	ErrNoCallSource = errors.New("conversation: no call source configured")

	ErrConversationIDRequired = ErrEntryIDRequired
)
