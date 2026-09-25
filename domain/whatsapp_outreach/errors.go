package whatsapp_outreach

import "errors"

var (
	ErrInvalidPhone          = errors.New("whatsapp outreach: not a valid phone number")
	ErrBusinessPhoneNotFound = errors.New("whatsapp outreach: business phone not found")
	ErrPhoneNotConnected     = errors.New("whatsapp outreach: this number is not connected")
	ErrTemplateNotFound      = errors.New("whatsapp outreach: template not found")
	ErrLeadBlocked           = errors.New("whatsapp outreach: this contact is blocked")
	ErrWindowAlreadyOpen     = errors.New("whatsapp outreach: this conversation is already open, reply for free instead")
	ErrWithinSpamWindow      = errors.New("whatsapp outreach: this contact was messaged from this number too recently")
	ErrDepartmentForbidden   = errors.New("whatsapp outreach: this number belongs to another department")
	ErrTemplateForbidden     = errors.New("whatsapp outreach: this template is not available to this workspace")
	ErrRateLimited           = errors.New("whatsapp outreach: too many new conversations started from this workspace, try again shortly")
	ErrConversationNotFound  = errors.New("whatsapp outreach: conversation not found")
	ErrSendOutcomeUnknown    = errors.New("whatsapp outreach: the template may have been delivered, the provider's answer was lost")
)
