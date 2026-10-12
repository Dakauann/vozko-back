package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vozko/domain/ai"
)

type benchFlags struct {
	Inputs     string
	Out        string
	Front      string
	Models     []string
	Tracks     []string
	Arms       []string
	Only       []string
	Counted    []string
	Label      string
	Workers    int
	Cap        float64
	CeilingUSD float64
}

type costRecord struct {
	CostUSD float64 `json:"cost_usd"`
}

func spentIn(paths []string) (float64, error) {
	total := 0.0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		var records []costRecord
		if err := json.Unmarshal(raw, &records); err != nil {
			return 0, fmt.Errorf("%s: %w", path, err)
		}
		for _, r := range records {
			total += r.CostUSD
		}
	}
	return total, nil
}

func readJSON(path string, into interface{}) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func writeJSON(path string, value interface{}) error {
	raw, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func visionModel(ctx context.Context, service ai.Service, model string) (ai.ModelInfo, error) {
	limits, err := ai.ResolveAgentModel(ctx, service, model, "")
	if err != nil {
		return limits, err
	}
	if !limits.SeesImages() {
		return limits, errors.New("the model catalog says it does not see images")
	}
	return limits, nil
}

func benchMain(service ai.Service, ledger *referenceLedger, flags benchFlags) int {
	if len(flags.Models) == 0 || flags.Cap <= 0 || flags.Cap > benchHardCap || flags.CeilingUSD <= 0 {
		fmt.Fprintf(os.Stderr, "the design bench needs -models and a -cap up to US$ %.0f\n", benchHardCap)
		return 1
	}
	var inputs benchInputs
	if err := readJSON(flags.Inputs, &inputs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	jobs, err := benchJobs(inputs, filepath.Dir(flags.Inputs), flags.Tracks, flags.Models, flags.Arms, flags.Only)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for i := range jobs {
		jobs[i].Label = flags.Label
	}
	var done []benchRun
	if err := readJSON(flags.Out, &done); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	spent, err := spentIn(flags.Counted)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx := context.Background()
	limits := map[string]ai.ModelInfo{}
	session := benchSession{service: service, ledger: ledger, project: inputs.Project, ceilingUSD: flags.CeilingUSD}
	plan := benchPlan{
		Jobs:    jobs,
		Cap:     flags.Cap,
		Spent:   spent,
		Reserve: flags.CeilingUSD,
		Wait:    time.Sleep,
		Save:    func(runs []benchRun) error { return writeJSON(flags.Out, runs) },
		Limits: func(model string) (ai.ModelInfo, error) {
			if known, ok := limits[model]; ok {
				return known, nil
			}
			info, err := visionModel(ctx, service, model)
			if err == nil {
				limits[model] = info
			}
			return info, err
		},
		Bridge: func(perception string) (editorChannel, error) { return startBridge(flags.Front, perception, 0) },
		Run:    session.runJob,
		Progress: func(run benchRun) {
			fmt.Printf("%s %s %s perception %s %s: %d model calls, %d looks, %d/%d edits refused, US$ %.4f, %s\n", run.Track, run.Item, run.Model, run.Perception, run.Label, run.ModelCalls, run.Looks, run.FailedEdits, run.Edits, run.CostUSD, run.Outcome)
		},
	}
	if flags.Workers > 1 {
		return benchPoolMain(ctx, session, flags, jobs, done, spent, plan.Limits, tiersOf(inputs))
	}
	before := len(done)
	stop := runBench(ctx, plan, &done)
	fmt.Printf("%d new runs, US$ %.4f in this file, US$ %.4f with the other counted files\n", len(done)-before, spentOnRuns(done), spent+spentOnRuns(done))
	if stop != "" {
		fmt.Println(stop)
	}
	return 0
}

func judgeMain(service ai.Service, ledger *referenceLedger, inputsPath, outPath, model string, designers []string, counted []string, capUSD float64, workers int) int {
	if model == "" || capUSD <= 0 || capUSD > benchHardCap || len(designers) == 0 {
		fmt.Fprintf(os.Stderr, "the judge needs -judge-model, the bench results in -counted and a -cap up to US$ %.0f\n", benchHardCap)
		return 1
	}
	for _, designer := range designers {
		if designer == model {
			fmt.Fprintln(os.Stderr, "the judge must be a different model from the designers")
			return 1
		}
	}
	if _, err := visionModel(context.Background(), service, model); err != nil {
		fmt.Fprintf(os.Stderr, "%s cannot judge images: %v\n", model, err)
		return 1
	}
	spent, err := spentIn(counted)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	inputs, done, err := readPerception(inputsPath, outPath)
	if err == nil {
		inputs, err = withPictures(inputs, filepath.Dir(inputsPath))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if workers > 1 {
		return judgePoolMain(service, ledger, inputs, done, outPath, model, spent, capUSD, workers)
	}
	plan := perceptionPlan{Models: []string{model}, Cap: capUSD, Spent: spent, Wait: time.Sleep, Save: func(results []perceptionResult) error { return writePerception(outPath, results) }, Reported: ledger.total.take}
	before := len(done)
	stop := runPerception(context.Background(), service, inputs, plan, &done)
	fmt.Printf("%d new verdicts, US$ %.4f in this file, US$ %.4f with the other counted files\n", len(done)-before, spentOn(done), spent+spentOn(done))
	if stop != "" {
		fmt.Println(stop)
	}
	return 0
}

func runsIn(paths []string) []benchRun {
	var out []benchRun
	for _, path := range paths {
		var runs []benchRun
		if readJSON(path, &runs) != nil {
			continue
		}
		for _, run := range runs {
			if run.Model != "" && run.Track != "" {
				out = append(out, run)
			}
		}
	}
	return out
}

func designersIn(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, run := range runsIn(paths) {
		if !seen[run.Model] {
			seen[run.Model] = true
			out = append(out, run.Model)
		}
	}
	return out
}
