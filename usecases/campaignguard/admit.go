package campaignguard

import (
	"context"
	"time"

	"vozko/domain/campaign"
	"vozko/usecases/campaignqueue"
)

const HoldDelay = 5 * time.Second

type SkipWriter func(status campaign.SendStatus, code int, message string) error

type Admission struct {
	Admitted bool
	Result   campaignqueue.Result
	Reason   campaign.SkipReason
	Err      error
	release  func()
}

func (a Admission) Release() {
	if a.release != nil {
		a.release()
	}
}

func Admit(ctx context.Context, gate EntryGate, workspaceID, leadID, senderID string, writeSkip SkipWriter) Admission {
	if gate == nil || writeSkip == nil {
		return hold(ErrUnavailable, "")
	}
	reason, err := gate.Check(ctx, workspaceID, leadID, senderID)
	if err != nil {
		return hold(err, "")
	}
	if reason != "" {
		status, code, message := reason.Outcome()
		if err := writeSkip(status, code, message); err != nil {
			return hold(err, reason)
		}
		return Admission{Result: campaignqueue.Drop, Reason: reason}
	}
	release, err := gate.Claim(ctx, workspaceID, leadID, senderID)
	if err != nil {
		return hold(err, "")
	}
	return Admission{Admitted: true, release: release}
}

func hold(err error, reason campaign.SkipReason) Admission {
	return Admission{Result: campaignqueue.RetryLater(HoldDelay), Reason: reason, Err: err}
}
