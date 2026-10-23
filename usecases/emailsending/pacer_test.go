package emailsending

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

type pacerClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func (c *pacerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *pacerClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
	return nil
}

var pacerStart = time.Date(2026, 10, 21, 15, 0, 0, 250_000_000, time.UTC)

type pacerRig struct {
	state *memSendState
	clock *pacerClock
	ops   *fakeOps
	pacer *SendPacer
}

func newPacerRig(t *testing.T, quota *es.SendQuota) *pacerRig {
	t.Helper()
	r := &pacerRig{state: newMemSendState(), clock: &pacerClock{now: pacerStart}, ops: &fakeOps{}}
	if quota != nil {
		if err := saveSendQuota(r.state, *quota); err != nil {
			t.Fatal(err)
		}
	}
	pacer, err := NewSendPacer(PacerDeps{State: r.state, Ops: r.ops, Now: r.clock.Now, Sleep: r.clock.Sleep})
	if err != nil {
		t.Fatal(err)
	}
	r.pacer = pacer
	return r
}

func productionQuota(rate float64) *es.SendQuota {
	return &es.SendQuota{MaxSendRate: rate, Max24HourSend: 50000, ProductionAccess: true, Read: true, ReadAt: pacerStart.Add(-time.Minute)}
}

func campaignTicket(ws string) es.SendTicket {
	return es.SendTicket{WorkspaceID: ws, RunKind: es.RunCampaign}
}

func replyTicket(ws string) es.SendTicket {
	return es.SendTicket{WorkspaceID: ws, RunKind: es.RunReply}
}

func throttle(t *testing.T, err error) *es.SendError {
	t.Helper()
	var se *es.SendError
	if !errors.As(err, &se) || se.Kind != es.SendErrorTransient || se.RetryAfter <= 0 {
		t.Fatalf("err = %v, want a transient send error with a retry hint", err)
	}
	return se
}

func TestCampaignSendsShareTheProviderRateFairlyBetweenWorkspaces(t *testing.T) {
	r := newPacerRig(t, productionQuota(14))
	window := es.SendWindow(pacerStart)
	workspaces := []string{"ws-a", "ws-b", "ws-c"}
	if err := r.state.SAdd(campaignWorkspacesKey(window-1), workspaces...); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	granted := map[string]int64{}
	var wg sync.WaitGroup
	for _, ws := range workspaces {
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := r.pacer.Admit(context.Background(), campaignTicket(ws))
				if err != nil {
					if se := throttle(t, err); se.RetryAfter != es.UntilNextSendWindow(pacerStart) {
						t.Errorf("retry after = %s, want the next second", se.RetryAfter)
					}
					return
				}
				mu.Lock()
				granted[ws]++
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	var total int64
	for _, ws := range workspaces {
		want := es.FairShare(14, workspaces, ws, window)
		if granted[ws] != want {
			t.Errorf("%s sent %d, want its fair share %d", ws, granted[ws], want)
		}
		total += granted[ws]
	}
	if total != 14 {
		t.Fatalf("sent %d in one second, want the SES rate 14", total)
	}
	if got := r.state.count(sentBucketKey(pacerStart)); got != 14 {
		t.Fatalf("counted %d sends against the 24 hour quota, want 14", got)
	}
}

func TestMixedSendsNeverExceedTheProviderRateInOneSecond(t *testing.T) {
	r := newPacerRig(t, productionQuota(10))
	var mu sync.Mutex
	admitted := 0
	var wg sync.WaitGroup
	for i := range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticket := campaignTicket("ws-" + string(rune('a'+i%4)))
			if i%3 == 0 {
				ticket = replyTicket("ws-z")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := r.pacer.Admit(ctx, ticket); err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if admitted > 10 {
		t.Fatalf("admitted %d in one second, the SES rate is 10", admitted)
	}
	if got := r.state.count(totalKey(es.SendWindow(pacerStart))); got > 10 {
		t.Fatalf("token counter %d passed the rate", got)
	}
}

func TestAPrioritySendWaitsForTheNextSecondAndAsksCampaignsToLeaveItAToken(t *testing.T) {
	r := newPacerRig(t, productionQuota(1))
	window := es.SendWindow(pacerStart)
	if ok, _ := r.state.TryIncrBy(totalKey(window), 1, 1); !ok {
		t.Fatal("seed")
	}
	if err := r.pacer.Admit(context.Background(), replyTicket("ws-a")); err != nil {
		t.Fatalf("a reply waits a bounded time instead of failing: %v", err)
	}
	if len(r.clock.slept) != 1 || r.clock.slept[0] != es.UntilNextSendWindow(pacerStart) {
		t.Fatalf("slept = %v, want one wait to the next second", r.clock.slept)
	}
	if got := r.state.count(priorityWantedKey(window + 1)); got != 1 {
		t.Fatalf("priority demand for the next second = %d, want 1", got)
	}
}

func TestAPrioritySendGivesUpAfterTheBoundedWait(t *testing.T) {
	r := newPacerRig(t, productionQuota(1))
	window := es.SendWindow(pacerStart)
	for s := window; s <= window+int64(es.PriorityTokenWait/time.Second)+1; s++ {
		r.state.TryIncrBy(totalKey(s), 1, 1)
	}
	err := r.pacer.Admit(context.Background(), replyTicket("ws-a"))
	throttle(t, err)
	var waited time.Duration
	for _, d := range r.clock.slept {
		waited += d
	}
	if waited > es.PriorityTokenWait {
		t.Fatalf("waited %s, the bound is %s", waited, es.PriorityTokenWait)
	}
}

func TestCampaignSendsLeaveTheTokensPrioritySendsAskedFor(t *testing.T) {
	r := newPacerRig(t, productionQuota(2))
	window := es.SendWindow(pacerStart)
	if _, err := r.state.IncrWithTTL(priorityWantedKey(window), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := r.pacer.Admit(context.Background(), campaignTicket("ws-a")); err != nil {
		t.Fatalf("first campaign send: %v", err)
	}
	throttle(t, r.pacer.Admit(context.Background(), campaignTicket("ws-a")))
	if err := r.pacer.Admit(context.Background(), replyTicket("ws-b")); err != nil {
		t.Fatalf("the reply gets the token campaigns left: %v", err)
	}
	if len(r.clock.slept) != 0 {
		t.Fatalf("the reply should not have waited: %v", r.clock.slept)
	}
}

func TestTheDailyQuotaKeepsAReserveForPrioritySends(t *testing.T) {
	cases := []struct {
		name        string
		sesSent     float64
		ours        int64
		ticket      es.SendTicket
		wantRefused bool
	}{
		{name: "campaign under the reserve line", sesSent: 185, ours: 4, ticket: campaignTicket("ws-a")},
		{name: "campaign at the reserve line", sesSent: 185, ours: 5, ticket: campaignTicket("ws-a"), wantRefused: true},
		{name: "reply inside the reserve", sesSent: 195, ours: 4, ticket: replyTicket("ws-a")},
		{name: "reply at the quota", sesSent: 195, ours: 5, ticket: replyTicket("ws-a"), wantRefused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			quota := &es.SendQuota{MaxSendRate: 14, Max24HourSend: 200, SentLast24Hours: tc.sesSent, Read: true, ReadAt: pacerStart.Add(-time.Minute)}
			r := newPacerRig(t, quota)
			for range tc.ours {
				if err := countSent(r.state, pacerStart.Add(-time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			err := r.pacer.Admit(context.Background(), tc.ticket)
			if !tc.wantRefused {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if se := throttle(t, err); se.RetryAfter != es.DailyQuotaRetryAfter || !strings.Contains(se.Detail, "24 hour") {
				t.Fatalf("refusal = %+v", se)
			}
		})
	}
}

func TestAStaleQuotaHoldsCampaignsButLetsPrioritySendsThrough(t *testing.T) {
	quota := productionQuota(14)
	quota.ReadAt = pacerStart.Add(-es.SendQuotaStaleAfter - time.Minute)
	r := newPacerRig(t, quota)
	if se := throttle(t, r.pacer.Admit(context.Background(), campaignTicket("ws-a"))); se.RetryAfter != es.DailyQuotaRetryAfter {
		t.Fatalf("retry after = %s", se.RetryAfter)
	}
	if err := r.pacer.Admit(context.Background(), replyTicket("ws-a")); err != nil {
		t.Fatal(err)
	}
}

func TestBeforeTheFirstReadSendsArePacedAtTheSandboxRate(t *testing.T) {
	r := newPacerRig(t, nil)
	if err := r.pacer.Admit(context.Background(), replyTicket("ws-a")); err != nil {
		t.Fatal(err)
	}
	throttle(t, r.pacer.Admit(context.Background(), campaignTicket("ws-b")))
}

func TestAnUnreachableStoreHoldsCampaignsAndPacesPrioritySendsLocally(t *testing.T) {
	r := newPacerRig(t, productionQuota(14))
	r.state.failing.Store(true)
	throttle(t, r.pacer.Admit(context.Background(), campaignTicket("ws-a")))
	if err := r.pacer.Admit(context.Background(), replyTicket("ws-a")); err != nil {
		t.Fatalf("first reply: %v", err)
	}
	if err := r.pacer.Admit(context.Background(), replyTicket("ws-a")); err != nil {
		t.Fatalf("second reply waits for the local slot: %v", err)
	}
	var waited time.Duration
	for _, d := range r.clock.slept {
		waited += d
	}
	if waited < time.Second/es.SESSandboxMaxSendRate {
		t.Fatalf("waited %s between two replies without the store, want the sandbox interval", waited)
	}
	if len(r.ops.alerts) != 1 {
		t.Fatalf("alerts = %v, want one throttled alert", r.ops.alerts)
	}
}

func TestNewSendPacerRefusesMissingDependencies(t *testing.T) {
	if _, err := NewSendPacer(PacerDeps{Ops: &fakeOps{}}); err == nil {
		t.Fatal("a pacer without its store must not be built")
	}
	if _, err := NewSendPacer(PacerDeps{State: newMemSendState()}); err == nil {
		t.Fatal("a pacer without ops alerts must not be built")
	}
}
