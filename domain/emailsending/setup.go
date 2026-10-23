package emailsending

import (
	"errors"
	"time"
)

const SetupFireTopic = "email_sending.setup_fire"

type SetupWake interface {
	ScheduleFire(id string, fireAt time.Time) error
}

type SetupFailure string

const (
	SetupFailureProvider      SetupFailure = "provider_unavailable"
	SetupFailureReplyCapacity SetupFailure = "reply_capacity_reached"
)

func (f SetupFailure) Valid() bool {
	switch f {
	case "", SetupFailureProvider, SetupFailureReplyCapacity:
		return true
	}
	return false
}

func SetupFailureFor(err error) SetupFailure {
	if errors.Is(err, ErrInboundCapacityReached) {
		return SetupFailureReplyCapacity
	}
	return SetupFailureProvider
}

type SetupState string

const (
	SetupInProgress SetupState = "in_progress"
	SetupFailed     SetupState = "failed"
	SetupDone       SetupState = "done"
)

type SetupProgress struct {
	State   SetupState
	Failure SetupFailure
}

func (d SendingDomain) AwaitsSetup() bool {
	return d.Status == DomainPending && !d.Retired
}

func (d SendingDomain) SetupProgress() SetupProgress {
	switch {
	case !d.AwaitsSetup():
		return SetupProgress{State: SetupDone}
	case d.SetupFailure != "":
		return SetupProgress{State: SetupFailed, Failure: d.SetupFailure}
	}
	return SetupProgress{State: SetupInProgress}
}

func SetupProgressOf(domains []SendingDomain) SetupProgress {
	out := SetupProgress{State: SetupDone}
	for _, d := range domains {
		switch progress := d.SetupProgress(); progress.State {
		case SetupInProgress:
			return progress
		case SetupFailed:
			if out.State == SetupDone {
				out = progress
			}
		}
	}
	return out
}

func (d *SendingDomain) RestartSetup() bool {
	if d.Status == DomainPending && d.SetupFailure == "" && d.FailureReason == "" {
		return false
	}
	d.Status, d.SetupFailure, d.FailureReason = DomainPending, "", ""
	return true
}

func (d *SendingDomain) FailSetup(failure SetupFailure, reason string) {
	d.SetupFailure, d.FailureReason = failure, reason
	if d.Status != DomainVerified {
		d.Status = DomainPending
	}
}

func (d *SendingDomain) CompleteSetup(records []DNSRecord, layout DomainLayout, now time.Time) {
	d.Records = records
	d.MailFromDomain = layout.MailFromDomain
	d.ReplyDomain = layout.ReplyDomain
	d.SetupFailure, d.FailureReason = "", ""
	if d.Status != DomainVerified && d.Status != DomainVerifying {
		d.Status = DomainVerifying
		d.VerifyingSince = &now
	}
}

type ReleaseState string

const (
	ReleaseNone    ReleaseState = ""
	ReleasePending ReleaseState = "pending"
	ReleaseDone    ReleaseState = "done"
)

func (s ReleaseState) Valid() bool {
	switch s {
	case ReleaseNone, ReleasePending, ReleaseDone:
		return true
	}
	return false
}

func (d SendingDomain) AwaitsRelease() bool {
	return d.ReleaseState == ReleasePending
}

func (d *SendingDomain) Release() {
	d.Retired, d.ReleaseState = true, ReleasePending
}

func (d *SendingDomain) Released() {
	d.ReleaseState = ReleaseDone
}

func (d *SendingDomain) Reinstate() {
	d.Retired, d.ReleaseState = false, ReleaseNone
	d.VerifiedAt = nil
	d.RestartSetup()
}
