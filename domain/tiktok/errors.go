package tiktok

import "errors"

var (
	ErrAccountNotFound      = errors.New("tiktok account not found")
	ErrAccountAlreadyLinked = errors.New("tiktok account is already connected to another workspace")
	ErrNeedsReconnect       = errors.New("tiktok account needs to be reconnected")
	ErrAccountRevoked       = errors.New("tiktok account authorization was revoked")
	ErrAccountUnavailable   = errors.New("tiktok account is not connected or lacks the capability")
	ErrWorkspaceIDRequired  = errors.New("workspace id is required")
	ErrOpenIDInvalid        = errors.New("tiktok open id is missing or malformed")
	ErrInvalidStatus        = errors.New("invalid tiktok account status")
	ErrStatusTransition     = errors.New("invalid tiktok account status transition")
	ErrScopeMissing         = errors.New("tiktok grant has neither the comment nor the messaging scopes")
	ErrNotEligibleAccount   = errors.New("tiktok account type cannot use the business apis")
	ErrNotEligibleRegion    = errors.New("tiktok account region cannot use the business apis")
	ErrGrantIncomplete      = errors.New("tiktok grant is missing the token, the refresh token or the open id")
	ErrIdentityMismatch     = errors.New("tiktok token belongs to another account")

	ErrContactNotFound      = errors.New("tiktok contact not found")
	ErrConversationNotFound = errors.New("tiktok conversation not found")
	ErrVideoNotFound        = errors.New("tiktok video not found")
	ErrCommentNotFound      = errors.New("tiktok comment not found")
	ErrCommentNotOwned      = errors.New("tiktok comment does not belong to this account")

	ErrTextEmpty               = errors.New("tiktok message text is empty")
	ErrTextTooLong             = errors.New("tiktok message text exceeds 6000 characters")
	ErrCommentTextEmpty        = errors.New("tiktok comment reply is empty")
	ErrCommentTextTooLong      = errors.New("tiktok comment reply exceeds 1200 characters")
	ErrMediaUnsupported        = errors.New("tiktok accepts only JPG and PNG images in messages")
	ErrMediaTooLarge           = errors.New("tiktok message images are limited to 3 MB")
	ErrImageNotAllowedInMarket = errors.New("tiktok does not allow images in this conversation")
	ErrConversationIDMissing   = errors.New("tiktok conversation id is unknown, sends are refused")

	ErrInvalidEnvelope = errors.New("invalid tiktok webhook envelope")
	ErrInvalidContent  = errors.New("invalid tiktok webhook content")
	ErrUnknownEvent    = errors.New("unknown tiktok webhook event")

	ErrInteractiveUnsupported = errors.New("tiktok cards carry a title and one to three reply buttons")
	ErrInboxProfileInvalid    = errors.New("invalid tiktok inbox profile")
)
