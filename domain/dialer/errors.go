package dialer

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrSessionNotFound              = errors.New("dialer session not found")
	ErrSessionAlreadyActive         = errors.New("dialer session already active")
	ErrOwnerRequired                = errors.New("dialer owner is required")
	ErrTargetPhoneRequired          = errors.New("dialer target phone is required")
	ErrWorkspaceRequired            = errors.New("dialer workspace is required")
	ErrCallSourceNotConfigured      = errors.New("dialer call source not configured")
	ErrHistoryProviderNotConfigured = errors.New("dialer history provider not configured")
	ErrEntryFieldsRequired          = errors.New("dialer entry_id and entry_type are required")
	ErrEntryNotFound                = errors.New("dialer entry not found")
	ErrNoPhoneForEntry              = errors.New("dialer entry has no phone number")
	ErrAdmissionDependenciesMissing = errors.New("dialer admission dependencies missing")
	ErrTelephonyPricingUnavailable  = errors.New("dialer telephony pricing unavailable")
	ErrBalanceCheckFailed           = errors.New("dialer balance check failed")
	ErrReservationFailed            = errors.New("dialer inflight reservation failed")
	ErrInsufficientBalance          = errors.New("dialer insufficient balance")
	ErrNoCallSlotsAvailable         = errors.New("dialer no call slots available")
	ErrControlForbidden             = errors.New("dialer control forbidden")
	ErrInboundTrunkUnsupported      = errors.New("dialer inbound trunk unsupported")
	ErrInboundNoAvailableAgents     = errors.New("dialer inbound no available agents")
	ErrInboundOfferNotFound         = errors.New("dialer inbound offer not found")
	ErrInboundOfferNotForUser       = errors.New("dialer inbound offer not for user")
	ErrInboundOfferAlreadyResolved  = errors.New("dialer inbound offer already resolved")
	ErrInboundExecutorNotConfigured = errors.New("dialer inbound executor not configured")
)

const (
	TrunkBusyReasonReconciling = "reconciling"
	TrunkBusyReasonRegistering = "registering"
	TrunkBusyReasonNotOwned    = "not_owned"
)

type ErrTrunkBusy struct {
	TrunkID    string
	Reason     string
	RetryAfter time.Duration
}

func (e *ErrTrunkBusy) Error() string {
	if e == nil {
		return "<nil ErrTrunkBusy>"
	}
	reason := e.Reason
	if reason == "" {
		reason = TrunkBusyReasonNotOwned
	}
	return fmt.Sprintf("trunk %s busy: %s (retry after %s)", e.TrunkID, reason, e.RetryAfter)
}

func AsTrunkBusy(err error) *ErrTrunkBusy {
	if err == nil {
		return nil
	}
	var busy *ErrTrunkBusy
	if errors.As(err, &busy) {
		return busy
	}
	return nil
}
