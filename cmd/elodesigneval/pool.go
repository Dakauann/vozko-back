package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"vozko/domain/ai"
)

const poolSafety = 1.5

type poolPlan[J, R any] struct {
	Jobs     []J
	Key      func(J) string
	Group    func(J) string
	Tier     func(J) string
	Seed     map[string]map[string][]float64
	Floor    float64
	Cap      float64
	Spent    float64
	Workers  func(J) int
	Stopped  func() bool
	Wait     func(time.Duration)
	Run      func(ctx context.Context, worker int, job J) (R, float64, error)
	Record   func(R) error
	Progress func(R)
}

type poolOutcome[R any] struct {
	Done      []R
	Skipped   []string
	Cancelled []string
	Drained   int
	Stop      string
}

func (o poolOutcome[R]) drainLine() string {
	if len(o.Cancelled) == 0 {
		return ""
	}
	return fmt.Sprintf("stop requested: %d in-flight runs drained, %d queued runs cancelled", o.Drained, len(o.Cancelled))
}

func (o poolOutcome[R]) report() {
	for _, skipped := range o.Skipped {
		fmt.Println("not run:", skipped)
	}
	if line := o.drainLine(); line != "" {
		fmt.Println(line)
	}
	if o.Stop != "" {
		fmt.Println(o.Stop)
	}
}

type poolState struct {
	mu       sync.Mutex
	wake     *sync.Cond
	spent    float64
	reserved float64
	active   map[string]int
	limit    map[string]int
	costs    map[string]map[string][]float64
	stop     string
	draining bool
}

func mean(costs []float64) float64 {
	total := 0.0
	for _, c := range costs {
		total += c
	}
	return total / float64(len(costs))
}

func dearestMean(tiers map[string][]float64) (float64, bool) {
	dearest, seen := 0.0, false
	for _, costs := range tiers {
		if len(costs) > 0 {
			dearest, seen = max(dearest, mean(costs)), true
		}
	}
	return dearest, seen
}

func (s *poolState) estimate(group, tier string, floor float64) float64 {
	if costs := s.costs[group][tier]; len(costs) > 0 {
		return mean(costs) * poolSafety
	}
	if dearest, seen := dearestMean(s.costs[group]); seen {
		return dearest * poolSafety
	}
	return floor
}

func (s *poolState) observe(group, tier string, cost float64) {
	if s.costs[group] == nil {
		s.costs[group] = map[string][]float64{}
	}
	s.costs[group][tier] = append(s.costs[group][tier], cost)
}

func runPool[J, R any](ctx context.Context, plan poolPlan[J, R]) poolOutcome[R] {
	state := &poolState{spent: plan.Spent, active: map[string]int{}, limit: map[string]int{}, costs: map[string]map[string][]float64{}}
	state.wake = sync.NewCond(&state.mu)
	for group, tiers := range plan.Seed {
		for tier, costs := range tiers {
			for _, cost := range costs {
				state.observe(group, tier, cost)
			}
		}
	}
	var out poolOutcome[R]
	next := 0
	workers := 0
	for _, job := range plan.Jobs {
		workers = max(workers, plan.Workers(job))
		group := plan.Group(job)
		if _, ok := state.limit[group]; !ok {
			state.limit[group] = plan.Workers(job)
		}
	}
	drain := func() bool {
		if state.draining || next >= len(plan.Jobs) || !plan.Stopped() {
			return state.draining
		}
		state.draining = true
		for _, queued := range plan.Jobs[next:] {
			out.Cancelled = append(out.Cancelled, plan.Key(queued))
		}
		next = len(plan.Jobs)
		state.wake.Broadcast()
		return true
	}
	take := func() (J, float64, bool) {
		state.mu.Lock()
		defer state.mu.Unlock()
		var zero J
		for {
			if state.stop != "" || drain() || next >= len(plan.Jobs) {
				return zero, 0, false
			}
			job := plan.Jobs[next]
			group := plan.Group(job)
			if state.active[group] >= state.limit[group] {
				state.wake.Wait()
				continue
			}
			next++
			estimate := state.estimate(group, plan.Tier(job), plan.Floor)
			if state.spent+state.reserved+estimate > plan.Cap {
				out.Skipped = append(out.Skipped, fmt.Sprintf("%s (would pass the US$ %.2f cap: spent %.4f, reserved %.4f, estimate %.4f)", plan.Key(job), plan.Cap, state.spent, state.reserved, estimate))
				continue
			}
			state.active[group]++
			state.reserved += estimate
			return job, estimate, true
		}
	}
	finish := func(job J, estimate float64, result R, cost float64, ok bool, stop string) {
		state.mu.Lock()
		defer state.mu.Unlock()
		group := plan.Group(job)
		state.active[group]--
		state.reserved -= estimate
		state.spent += cost
		if ok {
			state.observe(group, plan.Tier(job), cost)
			out.Done = append(out.Done, result)
			if state.draining || state.stop == "" && drain() {
				out.Drained++
			}
			if err := plan.Record(result); err != nil && state.stop == "" {
				state.stop = fmt.Sprintf("stopped: the result of %s could not be saved: %v", plan.Key(job), err)
			}
			if plan.Progress != nil {
				plan.Progress(result)
			}
		}
		if stop != "" && state.stop == "" {
			state.stop = stop
		}
		state.wake.Broadcast()
	}
	halve := func(job J) {
		state.mu.Lock()
		defer state.mu.Unlock()
		group := plan.Group(job)
		state.limit[group] = max(1, state.limit[group]/2)
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				job, estimate, ok := take()
				if !ok {
					return
				}
				result, cost, err := plan.Run(ctx, worker, job)
				if wait, busy := ai.BusyRetryAfter(err); busy {
					halve(job)
					plan.Wait(max(wait, benchMinBackOff))
					var again float64
					result, again, err = plan.Run(ctx, worker, job)
					cost += again
				}
				stop := ""
				switch {
				case errors.Is(err, ai.ErrProviderBusy):
					stop = fmt.Sprintf("stopped: the provider is still busy after one back off on %s (%v)", plan.Key(job), err)
				case err != nil:
					stop = fmt.Sprintf("stopped on %s: %v", plan.Key(job), err)
				}
				finish(job, estimate, result, cost, true, stop)
			}
		}(w)
	}
	wg.Wait()
	out.Stop = state.stop
	return out
}
