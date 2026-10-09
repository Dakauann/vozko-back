package campaignguard

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/usecases/campaignqueue"
)

type fakeGate struct {
	reason   campaign.SkipReason
	checkErr error
	claimErr error
	claims   int
	released int
}

func (g *fakeGate) Check(context.Context, string, string, string) (campaign.SkipReason, error) {
	return g.reason, g.checkErr
}

func (g *fakeGate) Claim(context.Context, string, string, string) (func(), error) {
	if g.claimErr != nil {
		return nil, g.claimErr
	}
	g.claims++
	return func() { g.released++ }, nil
}

type skipLog struct {
	status  campaign.SendStatus
	code    int
	message string
	writes  int
	err     error
}

func (s *skipLog) write(status campaign.SendStatus, code int, message string) error {
	s.writes++
	s.status, s.code, s.message = status, code, message
	return s.err
}

func held(t *testing.T, a Admission) {
	t.Helper()
	if a.Admitted || a.Result.Outcome != campaignqueue.OutcomeRetryLater || a.Result.Delay != HoldDelay {
		t.Fatalf("admission = %+v, want the entry held for %v", a, HoldDelay)
	}
}

func TestAdmitLetsAnEligibleEntryThroughWithAClaim(t *testing.T) {
	gate := &fakeGate{}
	skips := &skipLog{}
	a := Admit(context.Background(), gate, "ws-1", "l-1", "bp-1", skips.write)
	if !a.Admitted || gate.claims != 1 || skips.writes != 0 {
		t.Fatalf("admission = %+v claims = %d writes = %d", a, gate.claims, skips.writes)
	}
	a.Release()
	if gate.released != 1 {
		t.Fatalf("released %d time(s), want once", gate.released)
	}
}

func TestAdmitDropsAnIneligibleEntryOnlyOnceTheSkipIsWritten(t *testing.T) {
	gate := &fakeGate{reason: campaign.SkipOptedOut}
	skips := &skipLog{}
	a := Admit(context.Background(), gate, "ws-1", "l-1", "bp-1", skips.write)
	if a.Admitted || a.Result.Outcome != campaignqueue.OutcomeDrop || a.Reason != campaign.SkipOptedOut {
		t.Fatalf("admission = %+v, want a drop for opted_out", a)
	}
	if skips.status != campaign.SendStatusFailed || skips.code != 920002 || skips.message != "opted_out" || gate.claims != 0 {
		t.Fatalf("skip = (%s, %d, %q) claims = %d", skips.status, skips.code, skips.message, gate.claims)
	}
	a.Release()

	unwritten := &skipLog{err: errBoom}
	a = Admit(context.Background(), &fakeGate{reason: campaign.SkipBlocked}, "ws-1", "l-1", "bp-1", unwritten.write)
	held(t, a)
	if !errors.Is(a.Err, errBoom) || a.Reason != campaign.SkipBlocked {
		t.Fatalf("admission = %+v, want the write error and the reason kept", a)
	}
}

func TestAdmitHoldsTheEntryWhenItCannotJudgeOrClaim(t *testing.T) {
	skips := &skipLog{}
	cases := map[string]Admission{
		"no gate":                        Admit(context.Background(), nil, "ws-1", "l-1", "bp-1", skips.write),
		"no skip writer":                 Admit(context.Background(), &fakeGate{}, "ws-1", "l-1", "bp-1", nil),
		"the check failed":               Admit(context.Background(), &fakeGate{checkErr: errBoom}, "ws-1", "l-1", "bp-1", skips.write),
		"another send holds the contact": Admit(context.Background(), &fakeGate{claimErr: ErrSendClaimed}, "ws-1", "l-1", "bp-1", skips.write),
		"the claim store failed":         Admit(context.Background(), &fakeGate{claimErr: errBoom}, "ws-1", "l-1", "bp-1", skips.write),
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			held(t, a)
			if a.Err == nil {
				t.Fatal("a held entry must say why")
			}
			a.Release()
		})
	}
	if skips.writes != 0 {
		t.Fatalf("a held entry must not be marked, %d writes", skips.writes)
	}
	if HoldDelay != 5*time.Second {
		t.Fatalf("HoldDelay = %v", HoldDelay)
	}
}
