package whatsapp_outreach

import "errors"

var (
	ErrInvalidPhone          = errors.New("whatsapp outreach: not a valid phone number")
	ErrBusinessPhoneNotFound = errors.New("whatsapp outreach: business phone not found")
	ErrPhoneNotConnected     = errors.New("whatsapp outreach: this number is not connected")
	ErrTemplateNotFound      = errors.New("whatsapp outreach: template not found")
	ErrLeadBlocked           = errors.New("whatsapp outreach: this contact is blocked")
	ErrLeadOptedOut          = errors.New("whatsapp outreach: this contact asked not to receive messages")
	ErrWindowAlreadyOpen     = errors.New("whatsapp outreach: this conversation is already open, reply for free instead")
	ErrWithinSpamWindow      = errors.New("whatsapp outreach: this contact was messaged from this number too recently")
	ErrDepartmentForbidden   = errors.New("whatsapp outreach: this number belongs to another department")
	ErrTemplateForbidden     = errors.New("whatsapp outreach: this template is not available to this workspace")
	ErrConversationNotFound  = errors.New("whatsapp outreach: conversation not found")
	ErrSendOutcomeUnknown    = errors.New("whatsapp outreach: the template may have been delivered, the provider's answer was lost")
)
