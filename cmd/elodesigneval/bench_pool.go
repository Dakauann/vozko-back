package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/ai"
)

const (
	strongWorkers  = 4
	judgeFloorUSD  = 0.05
	recordsSuffix  = ".runs"
	perceptionSlot = 20
	stopFileName   = "STOP"
)

func stopFilePath(out string) string {
	return filepath.Join(filepath.Dir(out), stopFileName)
}

func stopFile(path string) func() bool {
	return func() bool {
		_, err := os.Stat(path)
		return !errors.Is(err, os.ErrNotExist)
	}
}

func interrupted(interrupt <-chan struct{}) func() bool {
	return func() bool {
		select {
		case <-interrupt:
			return true
		default:
			return false
		}
	}
}

func anyStop(sources ...func() bool) func() bool {
	return func() bool {
		for _, stopped := range sources {
			if stopped() {
				return true
			}
		}
		return false
	}
}

func drainStop(out string) (func() bool, func()) {
	interrupt, release := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		<-interrupt.Done()
		release()
	}()
	return anyStop(interrupted(interrupt.Done()), stopFile(stopFilePath(out))), release
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func recordPath(dir, key string) string {
	return filepath.Join(dir, unsafeName.ReplaceAllString(key, "_")+".json")
}

func writeRecord(dir, key string, value interface{}) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeJSON(recordPath(dir, key), value)
}

func readRecords[R any](dir string) ([]R, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	out := make([]R, 0, len(names))
	for _, name := range names {
		var record R
		if err := readJSON(filepath.Join(dir, name), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func mergeRuns(done, records []benchRun) []benchRun {
	seen := map[string]bool{}
	for _, r := range done {
		seen[r.key()] = true
	}
	for _, r := range records {
		if !seen[r.key()] {
			done = append(done, r)
			seen[r.key()] = true
		}
	}
	return done
}

type workerBridges struct {
	mu      sync.Mutex
	front   string
	start   func(front, perception string, slot int) (editorChannel, error)
	open    map[int]map[string]editorChannel
	started []editorChannel
}

func (w *workerBridges) get(worker int, perception string) (editorChannel, error) {
	w.mu.Lock()
	current := w.open[worker][perception]
	w.mu.Unlock()
	if current != nil && current.usable() {
		return current, nil
	}
	if current != nil {
		current.close()
	}
	slot := worker
	if perception == perceptionOff {
		slot += perceptionSlot
	}
	bridge, err := w.start(w.front, perception, slot)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	if w.open[worker] == nil {
		w.open[worker] = map[string]editorChannel{}
	}
	w.open[worker][perception] = bridge
	w.started = append(w.started, bridge)
	w.mu.Unlock()
	return bridge, nil
}

func (w *workerBridges) closeAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, bridge := range w.started {
		bridge.close()
	}
}

func seedCosts[T any](records []T, group, tier func(T) string, cost func(T) float64) map[string]map[string][]float64 {
	out := map[string]map[string][]float64{}
	for _, r := range records {
		if out[group(r)] == nil {
			out[group(r)] = map[string][]float64{}
		}
		out[group(r)][tier(r)] = append(out[group(r)][tier(r)], cost(r))
	}
	return out
}

type tierIndex map[string]string

func tiersOf(inputs benchInputs) tierIndex {
	out := tierIndex{}
	for _, ref := range inputs.References {
		out[trackReplicate+":"+ref.ID] = ref.Tier
	}
	for _, brief := range inputs.Briefs {
		out[trackBrief+":"+brief.ID] = brief.Tier
	}
	return out
}

func (t tierIndex) of(track, item string) string {
	return t[track+":"+item]
}

func benchSeed(done []benchRun, tiers tierIndex) map[string]map[string][]float64 {
	return seedCosts(done, func(r benchRun) string { return r.Model }, func(r benchRun) string { return tiers.of(r.Track, r.Item) }, func(r benchRun) float64 { return r.CostUSD })
}

func benchHistory(counted []string, done []benchRun, tiers tierIndex) map[string]map[string][]float64 {
	return benchSeed(append(runsIn(counted), done...), tiers)
}

func benchPoolMain(ctx context.Context, session benchSession, flags benchFlags, jobs []benchJob, done []benchRun, spent float64, limits func(string) (ai.ModelInfo, error), tiers tierIndex) int {
	records := flags.Out + recordsSuffix
	leftover, err := readRecords[benchRun](records)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	done = mergeRuns(done, leftover)
	finished := map[string]bool{}
	for _, r := range done {
		finished[r.key()] = true
	}
	var pending []benchJob
	known := map[string]ai.ModelInfo{}
	for _, job := range jobs {
		if finished[job.key()] {
			continue
		}
		if _, ok := known[job.Model]; !ok {
			info, err := limits(job.Model)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s cannot design here: %v\n", job.Model, err)
				return 1
			}
			known[job.Model] = info
		}
		pending = append(pending, job)
	}
	bridges := &workerBridges{front: flags.Front, open: map[int]map[string]editorChannel{}, start: func(front, perception string, slot int) (editorChannel, error) {
		return startBridge(front, perception, slot)
	}}
	defer bridges.closeAll()
	stopped, release := drainStop(flags.Out)
	defer release()
	started := time.Now()
	outcome := runPool(ctx, poolPlan[benchJob, benchRun]{
		Jobs:  pending,
		Key:   benchJob.key,
		Group: func(job benchJob) string { return job.Model },
		Tier:  func(job benchJob) string { return tiers.of(job.Track, job.Item) },
		Seed:  benchHistory(flags.Counted, done, tiers),
		Floor: flags.CeilingUSD,
		Cap:   flags.Cap,
		Spent: spent + spentOnRuns(done),
		Workers: func(job benchJob) int {
			if known[job.Model].Has(ai.CapabilityLowPrice) {
				return flags.Workers
			}
			return min(flags.Workers, strongWorkers)
		},
		Stopped: stopped,
		Wait:    time.Sleep,
		Run: func(ctx context.Context, worker int, job benchJob) (benchRun, float64, error) {
			bridge, err := bridges.get(worker, job.Perception)
			if err != nil {
				return benchRun{Track: job.Track, Item: job.Item, Model: job.Model, Perception: job.Perception, Label: job.Label, Outcome: err.Error()}, 0, err
			}
			run, err := session.runJob(ctx, bridge, job, known[job.Model])
			return run, run.CostUSD, err
		},
		Record: func(run benchRun) error { return writeRecord(records, run.key(), run) },
		Progress: func(run benchRun) {
			fmt.Printf("%s %s %s perception %s %s: %d model calls, %d looks, %d/%d edits refused, US$ %.4f, %s (%s)\n", run.Track, run.Item, run.Model, run.Perception, run.Label, run.ModelCalls, run.Looks, run.FailedEdits, run.Edits, run.CostUSD, run.Outcome, time.Since(started).Round(time.Second))
		},
	})
	done = mergeRuns(done, outcome.Done)
	if err := writeJSON(flags.Out, done); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%d new runs in %s, US$ %.4f in this file, US$ %.4f with the other counted files\n", len(outcome.Done), time.Since(started).Round(time.Second), spentOnRuns(done), spent+spentOnRuns(done))
	outcome.report()
	return 0
}

func judgePoolMain(service ai.Service, ledger *referenceLedger, inputs perceptionInputs, done []perceptionResult, outPath, model string, spent, capUSD float64, workers int) int {
	records := outPath + recordsSuffix
	leftover, err := readRecords[perceptionResult](records)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, r := range leftover {
		if !finished(done, perceptionJob{CaseID: r.CaseID, Arm: r.Arm}, r.Model, ai.ReasoningLevel(r.Reasoning)) {
			done = append(done, r)
		}
	}
	var pending []perceptionJob
	for _, job := range inputs.Jobs {
		if !finished(done, job, model, "") {
			pending = append(pending, job)
		}
	}
	stopped, release := drainStop(outPath)
	defer release()
	started := time.Now()
	outcome := runPool(context.Background(), poolPlan[perceptionJob, perceptionResult]{
		Jobs:    pending,
		Key:     func(job perceptionJob) string { return job.CaseID + "|" + job.Arm },
		Group:   func(perceptionJob) string { return model },
		Tier:    func(perceptionJob) string { return "" },
		Seed:    seedCosts(done, func(r perceptionResult) string { return r.Model }, func(perceptionResult) string { return "" }, func(r perceptionResult) float64 { return r.CostUSD }),
		Floor:   judgeFloorUSD,
		Cap:     capUSD,
		Spent:   spent + spentOn(done),
		Workers: func(perceptionJob) int { return workers },
		Stopped: stopped,
		Wait:    time.Sleep,
		Run: func(ctx context.Context, _ int, job perceptionJob) (perceptionResult, float64, error) {
			input := perceptionInput(inputs.System, job, model, "")
			out, err := service.Generate(ctx, input)
			reported := ledger.takeFor(input.BillingReference)
			if err != nil {
				return perceptionResult{CaseID: job.CaseID, Arm: job.Arm, Model: model, CostUSD: float64(reported) / 1e6}, float64(reported) / 1e6, err
			}
			cost := float64(max(reported, out.Usage.ProviderCostMicros)) / 1e6
			return perceptionResult{CaseID: job.CaseID, Arm: job.Arm, Model: model, Text: out.Message.Content, CostUSD: cost, PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens}, cost, nil
		},
		Record: func(r perceptionResult) error { return writeRecord(records, r.CaseID+"|"+r.Arm+"|"+r.Model, r) },
	})
	done = append(done, outcome.Done...)
	if err := writePerception(outPath, done); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%d new verdicts in %s, US$ %.4f in this file, US$ %.4f with the other counted files\n", len(outcome.Done), time.Since(started).Round(time.Second), spentOn(done), spent+spentOn(done))
	outcome.report()
	return 0
}
