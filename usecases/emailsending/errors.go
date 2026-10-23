package emailsending

import "errors"

var (
	ErrDomainInvalid         = errors.New("emailsending: invalid domain")
	ErrDomainFreeMailbox     = errors.New("emailsending: free mailbox domains cannot be connected")
	ErrDomainShared          = errors.New("emailsending: the shared sending domains cannot be connected")
	ErrProviderUnavailable   = errors.New("emailsending: provider unavailable")
	ErrSharedDomainRemoval   = errors.New("emailsending: shared domains cannot be removed")
	ErrDomainInUse           = errors.New("emailsending: the domain still has senders")
	ErrDefaultSenderRequired = errors.New("emailsending: the workspace needs a default sender")
	ErrInvalidAddress        = errors.New("emailsending: invalid email address")
	ErrSuppressionNotFound   = errors.New("emailsending: address is not suppressed")
	ErrSuppressionLocked     = errors.New("emailsending: complaint and hard bounce suppressions cannot be removed")
	ErrNotInProbation        = errors.New("emailsending: account is not in probation")
	ErrNoInboundHandler      = errors.New("emailsending: no inbound handler registered")
	ErrNoUnsubscriber        = errors.New("emailsending: no unsubscriber registered")
	ErrEmptyNotification     = errors.New("emailsending: empty notification")
	ErrUndecodableEvent      = errors.New("emailsending: undecodable provider notification")
	ErrReplyCapacityReached  = errors.New("emailsending: the provider cannot take another reply domain")
)
