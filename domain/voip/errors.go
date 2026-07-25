package voip

import "errors"

var (
	ErrNoRTPPortAvailable = errors.New("no rtp port available")
	ErrCallNotFound       = errors.New("call not found")
	ErrInviteFailed       = errors.New("invite failed")
	ErrAcceptFailed       = errors.New("accept failed")

	ErrCallBusy        = errors.New("call busy (486)")
	ErrCallDeclined    = errors.New("call declined (603)")
	ErrCallNoAnswer    = errors.New("call no answer (480)")
	ErrCallNotFound_   = errors.New("call not found (404)")
	ErrCallCancelled   = errors.New("call cancelled (487)")
	ErrCallForbidden   = errors.New("call forbidden (403)")
	ErrCallUnavailable = errors.New("call unavailable (503)")
)
