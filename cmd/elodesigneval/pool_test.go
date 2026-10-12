package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/usecases/agentloop"
)

type poolScript struct {
	mu       sync.Mutex
	inFlight int32
	peak     int32
	runs     []string
	results  map[string][]error
	cost     float64
	waited   []time.Duration
	recorded []string
}

func (s *poolScript) plan(jobs []string, workers int, capUSD, spent float64, seed []float64) poolPlan[string, string] {
	return poolPlan[string, string]{
		Jobs:    jobs,
		Key:     func(j string) string { return j },
		Group:   func(string) string { return "m" },
		Tier:    func(string) string { return "" },
		Seed:    map[string]map[string][]float64{"m": {"": seed}},
		Floor:   1,
		Cap:     capUSD,
		Spent:   spent,
		Workers: func(string) int { return workers },
		Stopped: func() bool { return false },
		Wait: func(d time.Duration) {
			s.mu.Lock()
			s.waited = append(s.waited, d)
			s.mu.Unlock()
		},
		Run: func(_ context.Context, _ int, job string) (string, float64, error) {
			now := atomic.AddInt32(&s.inFlight, 1)
			for {
				peak := atomic.LoadInt32(&s.peak)
				if now <= peak || atomic.CompareAndSwapInt32(&s.peak, peak, now) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&s.inFlight, -1)
			s.mu.Lock()
			defer s.mu.Unlock()
			s.runs = append(s.runs, job)
			var err error
			if queue := s.results[job]; len(queue) > 0 {
				err, s.results[job] = queue[0], queue[1:]
			}
			return job, s.cost, err
		},
		Record: func(r string) error {
			s.mu.Lock()
			s.recorded = append(s.recorded, r)
			s.mu.Unlock()
			return nil
		},
	}
}

func names(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("j%d", i)
	}
	return out
}

func TestThePoolRunsInParallelUpToTheWorkerLimitAndRecordsEveryRun(t *testing.T) {
	script := &poolScript{cost: 0.01, results: map[string][]error{}}
	out := runPool(context.Background(), script.plan(names(12), 4, 20, 0, []float64{0.01}))
	if out.Stop != "" || len(out.Done) != 12 || len(script.recorded) != 12 {
		t.Fatalf("stop %q done %d recorded %d", out.Stop, len(out.Done), len(script.recorded))
	}
	if script.peak < 2 || script.peak > 4 {
		t.Fatalf("peak concurrency %d", script.peak)
	}
}

func TestThePoolNeverStartsARunThatCouldPassTheCapCountingReservations(t *testing.T) {
	script := &poolScript{cost: 0.4, results: map[string][]error{}}
	out := runPool(context.Background(), script.plan(names(10), 4, 3, 1, []float64{0.4}))
	spent := 1 + 0.4*float64(len(out.Done))
	if spent > 3 || len(out.Skipped) == 0 || len(out.Done)+len(out.Skipped) != 10 {
		t.Fatalf("spent %v done %d skipped %d", spent, len(out.Done), len(out.Skipped))
	}
}

func TestEachTierIsReservedFromItsOwnObservedAverage(t *testing.T) {
	state := &poolState{costs: map[string]map[string][]float64{"m": {"simple": {0.25, 0.75}, "complex": {1.0}}}}
	if got := state.estimate("m", "simple", 5); got != 0.5*poolSafety {
		t.Fatalf("simple estimate %v", got)
	}
	if got := state.estimate("m", "complex", 5); got != 1.0*poolSafety {
		t.Fatalf("complex estimate %v", got)
	}
}

func TestAnUnobservedTierReservesTheDearestObservedTierOfTheModel(t *testing.T) {
	state := &poolState{costs: map[string]map[string][]float64{"m": {"simple": {0.1, 0.3}, "complex": {1.0}}}}
	if got := state.estimate("m", "sota", 5); got != 1.0*poolSafety {
		t.Fatalf("unobserved tier estimate %v", got)
	}
	if got := state.estimate("other", "sota", 5); got != 5 {
		t.Fatalf("unobserved model estimate %v", got)
	}
}

func TestThePoolSkipsADearTierThatAPerModelAverageWouldHaveLetThrough(t *testing.T) {
	script := &poolScript{cost: 0.01, results: map[string][]error{}}
	plan := script.plan([]string{"cheap", "dear"}, 1, 1, 0, nil)
	plan.Tier = func(job string) string { return job }
	plan.Seed = map[string]map[string][]float64{"m": {"cheap": {0.1}, "dear": {1.0}}}
	out := runPool(context.Background(), plan)
	if len(out.Done) != 1 || out.Done[0] != "cheap" || len(out.Skipped) != 1 {
		t.Fatalf("done %v skipped %v", out.Done, out.Skipped)
	}
}

func TestBenchCostsAreSeededByTheTierTheInputsGiveEachItem(t *testing.T) {
	inputs := benchInputs{
		References: []benchReference{{ID: "menu", Tier: "simple"}},
		Briefs:     []benchBrief{{ID: "menu", Tier: "simple"}, {ID: "sota-poster", Tier: "complex"}},
	}
	tiers := tiersOf(inputs)
	done := []benchRun{
		{Track: trackBrief, Item: "menu", Model: "m", CostUSD: 0.1},
		{Track: trackBrief, Item: "sota-poster", Model: "m", CostUSD: 0.9},
		{Track: trackReplicate, Item: "menu", Model: "m", CostUSD: 0.2},
		{Track: trackBrief, Item: "unknown", Model: "m", CostUSD: 0.4},
	}
	seed := benchSeed(done, tiers)
	if fmt.Sprint(seed["m"]["simple"]) != "[0.1 0.2]" || fmt.Sprint(seed["m"]["complex"]) != "[0.9]" || fmt.Sprint(seed["m"][""]) != "[0.4]" {
		t.Fatalf("seed %v", seed)
	}
	if tiers.of(trackBrief, "sota-poster") != "complex" || tiers.of(trackReplicate, "sota-poster") != "" {
		t.Fatalf("tiers %v", tiers)
	}
}

func TestABusyProviderHalvesThePoolWaitsOnceAndStopsWhenStillBusy(t *testing.T) {
	busy := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", RetryAfter: 30 * time.Second, Err: errors.New("402")})
	script := &poolScript{cost: 0.01, results: map[string][]error{"j0": {busy, busy}}}
	out := runPool(context.Background(), script.plan(names(1), 4, 20, 0, nil))
	if out.Stop == "" || len(script.waited) != 1 || script.waited[0] < benchMinBackOff || len(out.Done) != 1 {
		t.Fatalf("stop %q waited %v done %d", out.Stop, script.waited, len(out.Done))
	}
}

func TestEveryRunGetsItsOwnRecordFileAndALaterStartMergesThem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runs")
	a := benchRun{Track: trackBrief, Item: "a", Model: "m/x", Perception: perceptionOn, Label: "2", CostUSD: 0.1}
	b := benchRun{Track: trackBrief, Item: "b", Model: "m/x", Perception: perceptionOn, Label: "2", CostUSD: 0.2}
	for _, r := range []benchRun{a, b} {
		if err := writeRecord(dir, r.key(), r); err != nil {
			t.Fatal(err)
		}
	}
	records, err := readRecords[benchRun](dir)
	if err != nil || len(records) != 2 {
		t.Fatalf("records %v err %v", records, err)
	}
	merged := mergeRuns([]benchRun{a}, records)
	if len(merged) != 2 || spentOnRuns(merged) < 0.299 {
		t.Fatalf("merged %+v", merged)
	}
}

func TestAStopRequestCancelsTheQueuedRunsAndDrainsTheInFlightOnes(t *testing.T) {
	script := &poolScript{cost: 0.01, results: map[string][]error{}}
	plan := script.plan(names(6), 2, 20, 0, []float64{0.01})
	var stopped atomic.Bool
	var started sync.WaitGroup
	started.Add(2)
	var cancelledContexts atomic.Int32
	run := plan.Run
	plan.Run = func(ctx context.Context, worker int, job string) (string, float64, error) {
		if job == "j0" || job == "j1" {
			started.Done()
			started.Wait()
			stopped.Store(true)
			time.Sleep(5 * time.Millisecond)
		}
		if ctx.Err() != nil {
			cancelledContexts.Add(1)
		}
		return run(ctx, worker, job)
	}
	plan.Stopped = stopped.Load
	out := runPool(context.Background(), plan)
	if out.Stop != "" || len(out.Done) != 2 || len(script.recorded) != 2 || out.Drained != 2 || len(out.Cancelled) != 4 {
		t.Fatalf("stop %q done %v recorded %v drained %d cancelled %v", out.Stop, out.Done, script.recorded, out.Drained, out.Cancelled)
	}
	if len(script.runs) != 2 || cancelledContexts.Load() != 0 {
		t.Fatalf("runs %v done %v cancelled contexts %d", script.runs, out.Done, cancelledContexts.Load())
	}
	if line := out.drainLine(); line != "stop requested: 2 in-flight runs drained, 4 queued runs cancelled" {
		t.Fatalf("drain line %q", line)
	}
}

func TestAPoolThatIsNeverStoppedReportsNoDrain(t *testing.T) {
	script := &poolScript{cost: 0.01, results: map[string][]error{}}
	out := runPool(context.Background(), script.plan(names(3), 2, 20, 0, []float64{0.01}))
	if out.Drained != 0 || len(out.Cancelled) != 0 || out.drainLine() != "" {
		t.Fatalf("drained %d cancelled %v line %q", out.Drained, out.Cancelled, out.drainLine())
	}
}

func TestTheStopFileSitsInTheOutDirectoryAndFailsClosed(t *testing.T) {
	if got := stopFilePath(filepath.Join(".out", "bench-results-r4.json")); got != filepath.Join(".out", "STOP") {
		t.Fatalf("stop file %q", got)
	}
	dir := t.TempDir()
	path := stopFilePath(filepath.Join(dir, "bench.json"))
	stopped := stopFile(path)
	if stopped() {
		t.Fatal("stopped without a stop file")
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !stopped() {
		t.Fatal("the stop file did not stop the pool")
	}
	if !stopFile("bad\x00path")() {
		t.Fatal("an unreadable stop file did not fail closed")
	}
}

func TestAnInterruptLatchesTheStop(t *testing.T) {
	interrupt := make(chan struct{})
	stopped := interrupted(interrupt)
	if stopped() {
		t.Fatal("stopped without an interrupt")
	}
	close(interrupt)
	if !stopped() || !stopped() {
		t.Fatal("the interrupt did not latch the stop")
	}
}

func TestAnyStopSourceStopsThePool(t *testing.T) {
	never := func() bool { return false }
	always := func() bool { return true }
	if anyStop(never, never)() || !anyStop(never, always)() {
		t.Fatal("anyStop did not combine its sources")
	}
}

func TestComplexTierHistoryFromCountedRoundsSeedsTheReservation(t *testing.T) {
	dir := t.TempDir()
	earlier := filepath.Join(dir, "bench-results-r4.json")
	runs := []benchRun{
		{Track: trackBrief, Item: "sota-poster", Model: "m", Perception: perceptionOn, Label: "4", CostUSD: 0.9},
		{Track: trackBrief, Item: "sota-poster", Model: "m", Perception: perceptionOff, Label: "4", CostUSD: 1.07},
	}
	if err := writeJSON(earlier, runs); err != nil {
		t.Fatal(err)
	}
	tiers := tiersOf(benchInputs{Briefs: []benchBrief{{ID: "sota-poster", Tier: "complex"}}})
	state := &poolState{costs: benchHistory([]string{earlier}, nil, tiers)}
	if got := state.estimate("m", "complex", 0.75); got < 1.07 {
		t.Fatalf("complex reservation %v is under the observed 0.9 to 1.07", got)
	}
}

func TestTheReferenceLedgerKeepsConcurrentRunsApart(t *testing.T) {
	ledger := newReferenceLedger(&usageLedger{})
	for _, event := range []string{`{"requestId":"run-a:1","providerCostMicros":100}`, `{"requestId":"run-b:1","providerCostMicros":40}`, `{"requestId":"run-a:2","providerCostMicros":5}`} {
		if err := ledger.Publish("t", []byte(event)); err != nil {
			t.Fatal(err)
		}
	}
	if a, b := ledger.takeFor("run-a"), ledger.takeFor("run-b"); a != 105 || b != 40 || ledger.takeFor("run-a") != 0 {
		t.Fatalf("a %d b %d", a, b)
	}
}
