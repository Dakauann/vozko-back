package callsession

import (
	"errors"
)

var (
	ErrOwnerRequired                = errors.New("call session owner is required")
	ErrCallIDRequired               = errors.New("call session call id is required")
	ErrTargetPhoneRequired          = errors.New("call session target phone is required")
	ErrWorkspaceRequired            = errors.New("call session workspace is required")
	ErrCallSourceNotConfigured      = errors.New("call session call source not configured")
	ErrHistoryProviderNotConfigured = errors.New("call session history provider not configured")
	ErrEntryFieldsRequired          = errors.New("call session entry_id and entry_type are required")
	ErrEntryNotFound                = errors.New("call session entry not found")
	ErrNoPhoneForEntry              = errors.New("call session entry has no phone number")
	ErrAdmissionDependenciesMissing = errors.New("call session admission dependencies missing")
	// ErrBillingNotConfigured is the call lifecycle refusing to exist without
	// the publisher that turns a completed call into a charge. Minutes spent at
	// the carrier and never billed cannot be recovered afterwards.
	ErrBillingNotConfigured        = errors.New("call session billing publisher is required")
	ErrTelephonyPricingUnavailable = errors.New("call session telephony pricing unavailable")
	ErrBalanceCheckFailed          = errors.New("call session balance check failed")
	ErrReservationFailed           = errors.New("call session inflight reservation failed")
	ErrInsufficientBalance         = errors.New("call session insufficient balance")
	ErrNoCallSlotsAvailable        = errors.New("call session no call slots available")
	ErrControlForbidden            = errors.New("call session control forbidden")
	// ErrSessionBusy: the target session already has a genuinely attached call, so
	// it cannot take another one.
	ErrSessionBusy                  = errors.New("call session already has an active call")
	ErrInboundNoAvailableAgents     = errors.New("call session inbound no available agents")
	ErrInboundOfferNotFound         = errors.New("call session inbound offer not found")
	ErrInboundOfferNotForUser       = errors.New("call session inbound offer not for user")
	ErrInboundOfferAlreadyResolved  = errors.New("call session inbound offer already resolved")
	ErrInboundExecutorNotConfigured = errors.New("call session inbound executor not configured")
)
