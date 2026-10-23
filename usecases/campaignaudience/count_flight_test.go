package campaignaudience_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"vozko/domain/campaignaudience"
	"vozko/domain/selection"
)

func leadsSpec() campaignaudience.Spec {
	return campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection()}
}

func (w *world) everyLeadReady(n int) {
	w.selections.ids = nil
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{}}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("lead-%06d", i)
		w.selections.ids = append(w.selections.ids, id)
		w.channel.judge.leads[id] = campaignaudience.Candidate{HasContact: true}
	}
}

func TestTenIdenticalCountsAtOnceWalkTheAudienceOnce(t *testing.T) {
	w := newWorld(t)
	w.everyLeadReady(12)
	w.selections.pause = make(chan struct{})
	var wg sync.WaitGroup
	counts := make([]campaignaudience.Count, 10)
	errs := make([]error, 10)
	for i := range counts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counts[i], errs[i] = w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, leadsSpec())
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(w.selections.pause)
	wg.Wait()
	for i := range counts {
		if errs[i] != nil || !reflect.DeepEqual(counts[i], counts[0]) || counts[i].Matched != 12 {
			t.Fatalf("count %d = %+v, %v", i, counts[i], errs[i])
		}
	}
	if w.selections.resolved != 1 || w.gate.acquired != 1 {
		t.Fatalf("resolved %d pages through %d gate slots, want one walk", w.selections.resolved, w.gate.acquired)
	}
}

func TestADifferentAudienceOrViewerCountsOnItsOwn(t *testing.T) {
	w := newWorld(t)
	w.everyLeadReady(3)
	other := testActor
	other.UserID = "user-2"
	excluded := leadsSpec()
	s := *excluded.Selection
	s.ExcludeIDs = []string{"lead-000000"}
	excluded.Selection = &s
	for _, call := range []struct {
		actor Actor
		spec  campaignaudience.Spec
	}{{testActor, leadsSpec()}, {other, leadsSpec()}, {testActor, excluded}} {
		if _, err := w.service.Count(ctx, call.actor, campaignaudience.ChannelWhatsApp, Params{}, call.spec); err != nil {
			t.Fatal(err)
		}
	}
	if w.selections.resolved != 3 {
		t.Fatalf("resolved = %d, want one walk each", w.selections.resolved)
	}
}

func TestACountRefusesWhenItCannotCoordinateTheRun(t *testing.T) {
	w := newWorld(t)
	w.everyLeadReady(3)
	w.state.Err = errors.New("redis down")
	if _, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, leadsSpec()); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("count = %v, want audience_unavailable", err)
	}
	if w.selections.resolved != 0 {
		t.Fatalf("the audience was walked unguarded %d times", w.selections.resolved)
	}
}

type cancelingJudge struct {
	fakeJudge
	cancel context.CancelFunc
}

func (j cancelingJudge) Leads(ctx context.Context, ids []string) ([]campaignaudience.Candidate, error) {
	j.cancel()
	return j.fakeJudge.Leads(ctx, ids)
}

type cancelingChannel struct {
	*fakeChannel
	judge Judge
}

func (c cancelingChannel) Prepare(ctx context.Context, a Actor, params Params) (Prepared, error) {
	prepared, err := c.fakeChannel.Prepare(ctx, a, params)
	prepared.Open = func(context.Context, []campaignaudience.Bound, campaignaudience.Source) (Judge, error) {
		return c.judge, nil
	}
	return prepared, err
}

func TestAnAbandonedCountStopsBeforeTheNextChunk(t *testing.T) {
	w := newWorld(t)
	w.everyLeadReady(selection.Chunk + 10)
	abandoned, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.service.AddChannel(cancelingChannel{fakeChannel: w.channel, judge: cancelingJudge{fakeJudge: w.channel.judge, cancel: cancel}})
	_, err := w.service.Count(abandoned, testActor, campaignaudience.ChannelWhatsApp, Params{}, leadsSpec())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("count = %v, want the cancellation", err)
	}
	if w.selections.resolved != 1 {
		t.Fatalf("resolved %d chunks after the caller left, want 1", w.selections.resolved)
	}
}

func TestNewRefusesWithoutACoordinatedRun(t *testing.T) {
	w := newWorld(t)
	deps := w.service.deps
	deps.Flight = nil
	if _, err := New(deps); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("New without a flight = %v", err)
	}
}
