package whatsapp_outreach

import "errors"

// Errors an operator can act on. Each one maps to a distinct thing the UI should
// say, which is why they are separate values rather than one wrapped message.
var (
	// ErrInvalidPhone is a number that cannot be addressed at all.
	ErrInvalidPhone = errors.New("whatsapp outreach: not a valid phone number")
	// ErrBusinessPhoneNotFound covers both "no such number" and "not yours".
	//
	// Deliberately one error: distinguishing them tells a caller which ids exist
	// in other workspaces, and there is nothing an operator can do differently
	// with the distinction.
	ErrBusinessPhoneNotFound = errors.New("whatsapp outreach: business phone not found")
	// ErrPhoneNotConnected is a number that exists but cannot send right now.
	ErrPhoneNotConnected = errors.New("whatsapp outreach: this number is not connected")
	// ErrTemplateNotFound is the same shape of answer for templates.
	ErrTemplateNotFound = errors.New("whatsapp outreach: template not found")
	// ErrLeadBlocked is a contact the workspace itself has blocked.
	ErrLeadBlocked = errors.New("whatsapp outreach: this contact is blocked")
	// ErrWindowAlreadyOpen means a free reply is possible, so charging for a
	// template would be spending money on something already available.
	ErrWindowAlreadyOpen = errors.New("whatsapp outreach: this conversation is already open, reply for free instead")
	// ErrWithinSpamWindow is the workspace's own cooldown refusing the send.
	ErrWithinSpamWindow = errors.New("whatsapp outreach: this contact was messaged from this number too recently")
	// ErrDepartmentForbidden is a number belonging to another department.
	ErrDepartmentForbidden = errors.New("whatsapp outreach: this number belongs to another department")
	// ErrTemplateForbidden is a template outside the workspace's access list.
	ErrTemplateForbidden = errors.New("whatsapp outreach: this template is not available to this workspace")
	// ErrRateLimited is the per-workspace hourly ceiling on cold outbound. It is
	// not a permission problem — it is pace.
	ErrRateLimited = errors.New("whatsapp outreach: too many new conversations started from this workspace, try again shortly")
)
