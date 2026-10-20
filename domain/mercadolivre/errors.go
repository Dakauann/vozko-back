package mercadolivre

import "errors"

var (
	ErrAccountNotFound      = errors.New("mercado livre account not found")
	ErrAccountAlreadyLinked = errors.New("mercado livre account is already connected to another workspace")
	ErrNotAccountAdmin      = errors.New("only the mercado livre account administrator can connect")
	ErrSiteNotSupported     = errors.New("mercado livre site is not supported")
	ErrPermissionMissing    = errors.New("mercado livre application is missing a functional permission")
	ErrNeedsReconnect       = errors.New("mercado livre account needs to be reconnected")
	ErrAccountRevoked       = errors.New("mercado livre account authorization was revoked")
	ErrWorkspaceIDRequired  = errors.New("workspace id is required")
	ErrUserIDInvalid        = errors.New("mercado livre user id must be a positive integer")
	ErrInvalidStatus        = errors.New("invalid mercado livre account status")
	ErrStatusTransition     = errors.New("invalid mercado livre account status transition")
	ErrCredentialsInvalid   = errors.New("mercado livre credentials need a user id and an access token")
	ErrScopeMissing         = errors.New("mercado livre grant is missing a required scope")

	ErrContactNotFound      = errors.New("mercado livre contact not found")
	ErrConversationNotFound = errors.New("mercado livre conversation not found")
	ErrListingNotFound      = errors.New("mercado livre listing not found")
	ErrQuestionNotFound     = errors.New("mercado livre question not found")
	ErrListingNotOwned      = errors.New("mercado livre listing does not belong to this account")

	ErrTextEmpty        = errors.New("mercado livre message text is empty")
	ErrTextTooLong      = errors.New("mercado livre message text exceeds 350 characters")
	ErrTextCharset      = errors.New("mercado livre message text has characters outside ISO-8859-1 and the emoji list")
	ErrAnswerEmpty      = errors.New("mercado livre answer text is empty")
	ErrAnswerTooLong    = errors.New("mercado livre answer text exceeds 2000 characters")
	ErrMediaUnsupported = errors.New("mercado livre accepts only JPG, PNG, PDF and TXT attachments with a plain file name")
	ErrMediaTooLarge    = errors.New("mercado livre attachments are limited to 25 MB")

	ErrMediationUnknown    = errors.New("mercado livre thread mediation is unknown, sends are refused")
	ErrBuyerUnknown        = errors.New("mercado livre thread buyer is unknown, sends are refused")
	ErrPackUnknown         = errors.New("mercado livre pack and order ids are both missing")
	ErrUnknownThreadStatus = errors.New("unknown mercado livre conversation status")
	ErrUnknownSubstatus    = errors.New("unknown mercado livre conversation substatus")

	ErrQuestionNotOpen       = errors.New("mercado livre question is no longer unanswered")
	ErrQuestionNotOwned      = errors.New("mercado livre question does not belong to this account")
	ErrUnknownQuestionStatus = errors.New("unknown mercado livre question status")
	ErrUnknownAnswerStatus   = errors.New("unknown mercado livre answer status")

	ErrInvalidNotification  = errors.New("mercado livre notification is not valid json")
	ErrWrongApplication     = errors.New("mercado livre notification belongs to another application")
	ErrUnknownTopic         = errors.New("mercado livre notification topic is not handled")
	ErrInvalidResource      = errors.New("mercado livre notification resource does not match its topic")
	ErrUnknownAction        = errors.New("mercado livre notification action is not handled")
	ErrNotificationIdentity = errors.New("mercado livre notification carries no id and no timestamp to deduplicate on")
)
