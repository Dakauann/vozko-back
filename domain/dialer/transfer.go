package dialer

import (
	"errors"
	"fmt"
	"time"
)

type TransferKind string

const (
	TransferKindBlind TransferKind = "blind"

	TransferKindAttended TransferKind = "attended"
)

func (k TransferKind) Valid() bool {
	switch k {
	case TransferKindBlind, TransferKindAttended:
		return true
	}
	return false
}

type TransferStage string

const (
	TransferStagePendingOffer TransferStage = "pending_offer"
	TransferStageConsulting   TransferStage = "consulting"
	// TransferStageCompleting is the transient window between "a party accepted"
	// and "the media actually moved". Observers (AwaitResolution, the roulette)
	// treat it as still in flight, so a swap that fails after the accept can never
	// be reported as a completed transfer (it rolls forward to failed/recalling
	// instead). See plan §4.4 (fixes the stage-flips-before-swap race).
	TransferStageCompleting TransferStage = "completing"
	// TransferStageRecalling is the Asterisk "Recall superstate": the transfer
	// failed (decline / timeout) while the caller leg is PARKED, so the engine is
	// ringing the original initiator to take the call back, bounded by the recall
	// ladder (attempts + delay) and the park deadline.
	TransferStageRecalling TransferStage = "recalling"
	// TransferStageQueued is the ACD waiting line: the caller leg is PARKED (same as
	// recalling) but the engine rings a POOL of agents (a department, one agent, or
	// the whole workspace) in waves as they free up, instead of the original
	// initiator. It is the generalization of the recall superstate; bounded by the
	// queue policy (MaxWait via ParkDeadline, MaxLength via the QueueDirector,
	// overflow action). Non-terminal: it resolves to completing (accepted) or failed
	// (overflow / abandon).
	TransferStageQueued    TransferStage = "queued"
	TransferStageCompleted TransferStage = "completed"
	TransferStageDeclined  TransferStage = "declined"
	TransferStageTimedOut  TransferStage = "timed_out"
	TransferStageCancelled TransferStage = "cancelled"
	// TransferStageRecalled is the terminal for a recall that succeeded: the
	// transfer did NOT complete, but the caller was safely returned to (re-attached
	// on) the original initiator.
	TransferStageRecalled TransferStage = "recalled"
	// TransferStageFailed is the terminal for every outcome where the transfer
	// neither completed nor returned the caller to a human: customer hangup mid
	// transfer, recall ladder exhausted, executor wedged. FailReason carries why.
	TransferStageFailed TransferStage = "failed"
)

func (s TransferStage) Terminal() bool {
	switch s {
	case TransferStageCompleted, TransferStageDeclined, TransferStageTimedOut,
		TransferStageCancelled, TransferStageRecalled, TransferStageFailed:
		return true
	}
	return false
}

func ValidateStageTransition(current, next TransferStage) error {
	if current == next {
		return ErrTransferInvalidStage
	}
	if current.Terminal() {
		return ErrTransferInvalidStage
	}
	switch current {
	case TransferStagePendingOffer:
		switch next {
		case TransferStageConsulting, TransferStageCompleting, TransferStageRecalling, TransferStageQueued,
			TransferStageDeclined, TransferStageTimedOut, TransferStageCancelled, TransferStageFailed:
			return nil
		}
	case TransferStageConsulting:
		switch next {
		case TransferStageCompleting, TransferStageCancelled, TransferStageFailed:
			return nil
		}
	case TransferStageCompleting:
		switch next {
		case TransferStageCompleted, TransferStageRecalled, TransferStageRecalling, TransferStageQueued, TransferStageFailed:
			return nil
		}
	case TransferStageRecalling:
		switch next {
		case TransferStageCompleting, TransferStageCancelled, TransferStageFailed:
			return nil
		}
	case TransferStageQueued:
		// A queued caller is dequeued into completing (a wave was accepted; the
		// media swap decides the terminal), torn down to failed (overflow / abandon /
		// max wait) or cancelled, or (overflow action = recall) handed to the recall
		// ladder to ring the original initiator. A wave whose swap fails re-enters
		// queued from completing (mirrors the recall re-entry).
		switch next {
		case TransferStageCompleting, TransferStageFailed, TransferStageCancelled, TransferStageRecalling:
			return nil
		}
	}
	return ErrTransferInvalidStage
}

type TransferRequest struct {
	WorkspaceID  string
	InitiatorID  string
	TargetUserID string
	CallID       string
	Kind         TransferKind
	Note         string
}

func (r TransferRequest) Validate() error {
	if r.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if r.InitiatorID == "" {
		return ErrOwnerRequired
	}
	if r.TargetUserID == "" {
		return ErrTransferTargetRequired
	}
	if r.InitiatorID == r.TargetUserID {
		return ErrTransferSelfTransfer
	}
	if r.CallID == "" {
		return ErrTransferCallNotFound
	}
	if !r.Kind.Valid() {
		return ErrTransferInvalidKind
	}
	return nil
}

type TransferHandle struct {
	ID           string        `json:"id"`
	CallID       string        `json:"call_id"`
	WorkspaceID  string        `json:"workspace_id"`
	InitiatorID  string        `json:"initiator_id"`
	TargetUserID string        `json:"target_user_id"`
	Kind         TransferKind  `json:"kind"`
	Stage        TransferStage `json:"stage"`
	Note         string        `json:"note,omitempty"`
	// OfferedSessionIDs is every contact the CURRENT ring wave forked to (a member's
	// browser AND their branch ring together). AcceptedSessionID is the single contact
	// that won the first-to-answer race (the stage compare-and-set); the others are
	// cancelled. OfferedUserID is the member that wave targets: the transfer target
	// while pending, the ORIGINAL INITIATOR while recalling (empty means the target,
	// for handles created before recall existed).
	OfferedSessionIDs []string `json:"offered_session_ids,omitempty"`
	AcceptedSessionID string   `json:"accepted_session_id,omitempty"`
	OfferedUserID     string   `json:"offered_user_id,omitempty"`

	// Parked reports the caller leg was detached from the initiator and parked with
	// hold audio (true blind / blond): the initiator is free, the leg is owned by the
	// park registry keyed by this transfer's ID, and failure paths recall instead of
	// passively leaving the caller with the initiator.
	Parked bool `json:"parked,omitempty"`
	// EarlyCompleted marks an attended transfer the initiator completed while the
	// target was still ringing (the Asterisk "blond" transfer): from that point the
	// pending offer behaves exactly like a parked blind transfer.
	EarlyCompleted bool `json:"early_completed,omitempty"`

	// FailReason says why Stage is failed (customer_hangup, recall_exhausted,
	// recall_declined, swap_failed, executor_wedged, park_deadline).
	FailReason string `json:"fail_reason,omitempty"`

	// Recall ladder state (stage recalling): how many recall waves rang the
	// initiator, when the outstanding wave started (zero = none outstanding), and
	// the hard deadline after which a parked caller is never kept waiting.
	RecallAttempts int       `json:"recall_attempts,omitempty"`
	RecallOfferAt  time.Time `json:"recall_offer_at,omitempty"`
	ParkDeadline   time.Time `json:"park_deadline,omitempty"`

	// Queue state (stage queued): the waiting line this caller is in and when they
	// joined it. The per-wave bookkeeping reuses OfferedSessionIDs + RecallOfferAt
	// (queued and recalling are mutually exclusive stages) and the hard max-wait
	// ceiling reuses ParkDeadline, so no new wave/timing fields are needed.
	QueueKind  QueueTargetKind `json:"queue_kind,omitempty"`
	QueueID    string          `json:"queue_id,omitempty"`
	EnqueuedAt time.Time       `json:"enqueued_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RingsUserID is the member the current ring wave targets: the transfer target
// while the offer is pending, the original initiator during a recall.
func (h *TransferHandle) RingsUserID() string {
	if h == nil {
		return ""
	}
	if h.OfferedUserID != "" {
		return h.OfferedUserID
	}
	return h.TargetUserID
}

// QueueTarget reconstructs the waiting-line selector for a queued handle.
func (h *TransferHandle) QueueTarget() QueueTarget {
	if h == nil {
		return QueueTarget{}
	}
	return QueueTarget{Kind: h.QueueKind, ID: h.QueueID}
}

// AsQueuedCaller is the QueueDirector's view of this handle while it is queued.
func (h *TransferHandle) AsQueuedCaller(phone string) QueuedCaller {
	if h == nil {
		return QueuedCaller{}
	}
	return QueuedCaller{
		TransferID:  h.ID,
		WorkspaceID: h.WorkspaceID,
		CallID:      h.CallID,
		Target:      h.QueueTarget(),
		Phone:       phone,
		EnqueuedAt:  h.EnqueuedAt,
	}
}

var (
	ErrTransferTargetRequired  = errors.New("dialer transfer: target user is required")
	ErrTransferTargetOffline   = errors.New("dialer transfer: target user not connected to dialer")
	ErrTransferTargetBusy      = errors.New("dialer transfer: target user already on a call")
	ErrTransferSelfTransfer    = errors.New("dialer transfer: cannot transfer to self")
	ErrTransferCallNotFound    = errors.New("dialer transfer: call not found")
	ErrTransferNotOwner        = errors.New("dialer transfer: initiator is not the current owner of this call")
	ErrTransferNotFound        = errors.New("dialer transfer: transfer not found")
	ErrTransferTimedOut        = errors.New("dialer transfer: target did not answer in time")
	ErrTransferAlreadyInFlight = errors.New("dialer transfer: another transfer is already in flight for this call")
	ErrTransferInvalidKind     = errors.New("dialer transfer: invalid kind")
	ErrTransferInvalidStage    = errors.New("dialer transfer: invalid stage transition")
	ErrTransferNotForUser      = errors.New("dialer transfer: action not permitted for this user")
)

type TransferReason string

const (
	TransferReasonInitiatorDisconnect TransferReason = "initiator_disconnect"
	TransferReasonTargetDisconnect    TransferReason = "target_disconnect"
	TransferReasonTargetDeclined      TransferReason = "target_declined"
	TransferReasonInitiatorCancelled  TransferReason = "initiator_cancelled"
	TransferReasonCustomerHangup      TransferReason = "customer_hangup"
	TransferReasonOfferTimeout        TransferReason = "offer_timeout"

	// Recall ladder / park outcomes (FailReason values on TransferStageFailed).
	TransferReasonRecallDeclined  TransferReason = "recall_declined"
	TransferReasonRecallExhausted TransferReason = "recall_exhausted"
	TransferReasonParkDeadline    TransferReason = "park_deadline"
	TransferReasonSwapFailed      TransferReason = "swap_failed"
	TransferReasonExecutorWedged  TransferReason = "executor_wedged"

	// TransferReasonQueueOverflow is the FailReason when a queued caller hit the
	// policy's MaxWait ceiling (or arrived at a full line) and the overflow action
	// ran (announce + hangup). The caller is never left in infinite hold audio.
	TransferReasonQueueOverflow TransferReason = "queue_overflow"
)

type TransferError struct {
	Err    error
	Reason string
}

func (e *TransferError) Error() string {
	if e == nil {
		return "<nil TransferError>"
	}
	if e.Reason == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Err.Error(), e.Reason)
}

func (e *TransferError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
