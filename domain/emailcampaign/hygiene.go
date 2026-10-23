package emailcampaign

import (
	"errors"
	"time"

	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

const (
	HoldTestBatchBounces    HoldReason = "test_batch_bounces"
	HoldTestBatchComplaints HoldReason = "test_batch_complaints"
)

var ErrTestBatchFailed = errors.New("emailcampaign: the test batch failed, the campaign can only be stopped")

func (h HoldReason) FailedTestBatch() bool {
	return h == HoldTestBatchBounces || h == HoldTestBatchComplaints
}

var verdictSkips = map[emailhygiene.Verdict]SkipReason{
	emailhygiene.VerdictSuppressed:    SkipSuppressed,
	emailhygiene.VerdictBadAddress:    SkipBadAddress,
	emailhygiene.VerdictNoMailDomain:  SkipNoMailDomain,
	emailhygiene.VerdictDisposable:    SkipDisposable,
	emailhygiene.VerdictDomainUnknown: SkipDomainUnknown,
	emailhygiene.VerdictDomainPending: SkipDomainChecking,
	emailhygiene.VerdictInactive:      SkipInactive,
}

func HygieneSkip(f emailhygiene.Facts, s emailhygiene.Standing, now time.Time) SkipReason {
	verdict := emailhygiene.Assess(f, s, now)
	if !verdict.BlocksCampaigns() {
		return SkipNone
	}
	return verdictSkips[verdict]
}

func (c Campaign) Resumable() error {
	if c.HoldReason.FailedTestBatch() {
		return ErrTestBatchFailed
	}
	return nil
}

func (c Campaign) Actions() []Command {
	if !c.HoldReason.FailedTestBatch() {
		return AllowedActions(c.Status)
	}
	out := []Command{}
	if transitions(c.Status, ActionStop) {
		out = append(out, CommandStop)
	}
	return out
}

type TestBatchState string

const (
	TestBatchNone    TestBatchState = ""
	TestBatchTesting TestBatchState = "testing"
	TestBatchPassed  TestBatchState = "passed"
	TestBatchFailed  TestBatchState = "failed"
)

func (s TestBatchState) Valid() bool {
	switch s {
	case TestBatchNone, TestBatchTesting, TestBatchPassed, TestBatchFailed:
		return true
	}
	return false
}

type TestBatch struct {
	Size      int
	State     TestBatchState
	LastID    string
	DrainedAt *time.Time
	DecidedAt *time.Time
}

func (t *TestBatch) Holding() bool {
	return t != nil && t.State == TestBatchTesting
}

const (
	TestBatchFreshBasisPoints    = 2000
	TestBatchMinAudience         = 200
	TestBatchShareBasisPoints    = 1000
	TestBatchMinSize             = 200
	TestBatchMaxSize             = int(BillingBatchSize)
	TestBatchMinWait             = 10 * time.Minute
	TestBatchMaxWait             = time.Hour
	TestBatchKnownBasisPoints    = 9500
	TestBatchMaxBounceBasisPts   = 300
	TestBatchMaxComplaintBasisPt = 10
)

func PlanTestBatch(sendable, fresh int) int {
	if sendable <= TestBatchMinAudience || fresh*10000 < TestBatchFreshBasisPoints*sendable {
		return 0
	}
	size := (sendable*TestBatchShareBasisPoints + 9999) / 10000
	return min(max(size, TestBatchMinSize), TestBatchMaxSize, sendable)
}

type TestBatchOutcome struct {
	Sent        int64
	Delivered   int64
	HardBounces int64
	Complaints  int64
	Drained     bool
	DrainedAt   time.Time
}

type GateDecision string

const (
	GateWait           GateDecision = "wait"
	GatePass           GateDecision = "pass"
	GateFailBounces    GateDecision = "fail_bounces"
	GateFailComplaints GateDecision = "fail_complaints"
)

func (d GateDecision) Hold() HoldReason {
	switch d {
	case GateFailBounces:
		return HoldTestBatchBounces
	case GateFailComplaints:
		return HoldTestBatchComplaints
	}
	return HoldNone
}

func DecideTestBatch(o TestBatchOutcome, now time.Time) GateDecision {
	if !o.Drained {
		return GateWait
	}
	if o.HardBounces*10000 > TestBatchMaxBounceBasisPts*o.Sent {
		return GateFailBounces
	}
	if o.Complaints*10000 > TestBatchMaxComplaintBasisPt*o.Sent {
		return GateFailComplaints
	}
	waited := now.Sub(o.DrainedAt)
	if waited < TestBatchMinWait {
		return GateWait
	}
	known := o.Delivered + o.HardBounces
	if waited < TestBatchMaxWait && known*10000 < TestBatchKnownBasisPoints*o.Sent {
		return GateWait
	}
	return GatePass
}

func SkipForSuppression(r es.SuppressReason) SkipReason {
	switch r {
	case es.SuppressBadAddress:
		return SkipBadAddress
	case es.SuppressNoMailDomain:
		return SkipNoMailDomain
	}
	return SkipSuppressed
}

func (i IssueCode) ClearsOnItsOwn() bool {
	return i == IssueDomainsChecking
}

func (r LaunchReport) ReadyLater() bool {
	for _, issue := range r.Blocking {
		if !issue.Code.ClearsOnItsOwn() {
			return false
		}
	}
	return true
}

func (r LaunchReport) OnlyWaiting() bool {
	return !r.Ready() && r.ReadyLater()
}
