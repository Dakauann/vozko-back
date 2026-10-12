package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

func TestTheEditorProcessNeverInheritsTheBackendSecrets(t *testing.T) {
	env := secretFreeEnv([]string{"PATH=/bin", "OPENROUTER_API_KEY=sk-secret", "DATABASE_URL=postgres://x", "STUDIO_HARNESS_PORT=1"}, map[string]string{"OPENROUTER_API_KEY": "sk-secret", "DATABASE_URL": "postgres://x"}, bridgePort(0))
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "sk-secret") || strings.Contains(joined, "postgres://") || !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "STUDIO_HARNESS_PORT="+bridgePort(0)) || strings.Contains(joined, "STUDIO_HARNESS_PORT=1\n") {
		t.Fatalf("environment handed to the editor: %v", env)
	}
}

func benchFixture(t *testing.T) (benchInputs, string) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "refs", "menu.jpg"), []byte{0xff, 0xd8, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	return benchInputs{
		Project:    evalProject,
		Fonts:      []string{"inter", "anton"},
		References: []benchReference{{ID: "menu", Kind: "image", Open: benchOpen{Kind: "image", Width: 1080, Height: 1350}, Image: "refs/menu.jpg"}},
		Briefs:     []benchBrief{{ID: "promo", Open: benchOpen{Kind: "video", Aspect: "story"}, Text: "Vídeo do Café Gelado."}},
	}, dir
}

func TestTheReplicationJobCarriesOnlyThePictureTheSizeAndTheFonts(t *testing.T) {
	inputs, dir := benchFixture(t)
	jobs, err := benchJobs(inputs, dir, []string{trackReplicate, trackBrief}, []string{"strong", "cheap"}, []string{perceptionOn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 4 {
		t.Fatalf("jobs %d", len(jobs))
	}
	replica := jobs[0]
	if !strings.HasPrefix(replica.Image, "data:image/jpeg;base64,") || !strings.Contains(replica.Prompt, "1080 x 1350") || !strings.Contains(replica.Prompt, "inter, anton") || strings.Contains(replica.Prompt, "media_id") || strings.Contains(replica.Prompt, "BRASA") {
		t.Fatalf("replication job %+v", replica)
	}
	brief := jobs[3]
	if brief.Image != "" || !strings.Contains(brief.Prompt, "Café Gelado") || !strings.Contains(brief.Prompt, "story") {
		t.Fatalf("brief job %+v", brief)
	}
}

func TestTheJobListFollowsTheItemFilterAndRefusesUnknownArms(t *testing.T) {
	inputs, dir := benchFixture(t)
	jobs, err := benchJobs(inputs, dir, []string{trackReplicate, trackBrief}, []string{"m"}, []string{perceptionOn, perceptionOff}, []string{"brief:promo"})
	if err != nil || len(jobs) != 2 || jobs[0].Item != "promo" || jobs[1].Perception != perceptionOff {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
	if _, err := benchJobs(inputs, dir, []string{trackBrief}, []string{"m"}, []string{"maybe"}, nil); err == nil {
		t.Fatal("an unknown perception arm was accepted")
	}
}

type idleChannel struct{}

func (idleChannel) open(context.Context, string, json.RawMessage) error { return nil }

func (idleChannel) command(context.Context, copilot.ScreenCommand) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

func (idleChannel) document(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (idleChannel) usable() bool { return true }

func (idleChannel) close() {}

type scriptedBench struct {
	results  []error
	costs    []float64
	runs     []string
	bridges  []string
	waited   []time.Duration
	saved    int
	refusals map[string]error
}

func (s *scriptedBench) plan(jobs []benchJob, capUSD, spent float64) benchPlan {
	return benchPlan{
		Jobs:    jobs,
		Cap:     capUSD,
		Spent:   spent,
		Reserve: 1,
		Wait:    func(d time.Duration) { s.waited = append(s.waited, d) },
		Save:    func([]benchRun) error { s.saved++; return nil },
		Limits: func(model string) (ai.ModelInfo, error) {
			if err := s.refusals[model]; err != nil {
				return ai.ModelInfo{}, err
			}
			return ai.ModelInfo{ID: model}, nil
		},
		Bridge: func(perception string) (editorChannel, error) {
			s.bridges = append(s.bridges, perception)
			return idleChannel{}, nil
		},
		Run: func(_ context.Context, _ editorChannel, job benchJob, _ ai.ModelInfo) (benchRun, error) {
			s.runs = append(s.runs, job.key())
			n := len(s.runs) - 1
			run := benchRun{Track: job.Track, Item: job.Item, Model: job.Model, Perception: job.Perception, Outcome: "done"}
			if n < len(s.costs) {
				run.CostUSD = s.costs[n]
			}
			if n < len(s.results) {
				return run, s.results[n]
			}
			return run, nil
		},
	}
}

func jobsOf(n int, arm string) []benchJob {
	var jobs []benchJob
	for i := 0; i < n; i++ {
		jobs = append(jobs, benchJob{Track: trackBrief, Item: fmt.Sprintf("b%d", i), Model: "m", Perception: arm})
	}
	return jobs
}

func TestTheBenchStopsBeforeTheCapCountingEarlierSpend(t *testing.T) {
	script := &scriptedBench{costs: []float64{0.5, 0.5, 0.5}}
	var done []benchRun
	stop := runBench(context.Background(), script.plan(jobsOf(5, perceptionOn), 3, 1), &done)
	if stop == "" || len(done) != 3 || spentOnRuns(done)+1+1 <= 3 {
		t.Fatalf("stop %q, runs %d", stop, len(done))
	}
}

func TestTheBenchSkipsFinishedRunsAndOpensOneEditorPerPerceptionArm(t *testing.T) {
	script := &scriptedBench{}
	done := []benchRun{{Track: trackBrief, Item: "b0", Model: "m", Perception: perceptionOn}}
	jobs := append(jobsOf(2, perceptionOn), jobsOf(2, perceptionOff)...)
	if stop := runBench(context.Background(), script.plan(jobs, 20, 0), &done); stop != "" {
		t.Fatal(stop)
	}
	if len(script.runs) != 3 || strings.Join(script.bridges, ",") != "on,off" || len(done) != 4 {
		t.Fatalf("runs %v bridges %v done %d", script.runs, script.bridges, len(done))
	}
}

func TestTheBenchBacksOffOnceOnABusyProviderCarriesTheLostSpendAndThenStops(t *testing.T) {
	busy := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", RetryAfter: 30 * time.Second, Err: errors.New("402")})
	script := &scriptedBench{results: []error{busy, busy}, costs: []float64{0.2, 0.1}}
	var done []benchRun
	stop := runBench(context.Background(), script.plan(jobsOf(3, perceptionOn), 20, 0), &done)
	if stop == "" || len(script.waited) != 1 || script.waited[0] < benchMinBackOff || len(script.runs) != 2 {
		t.Fatalf("stop %q waited %v runs %v", stop, script.waited, script.runs)
	}
	if len(done) != 1 || done[0].CostUSD < 0.299 {
		t.Fatalf("the spend of the refused attempt was lost: %+v", done)
	}
}

func TestTheBenchStopsOnAProviderFailureWithoutRetrying(t *testing.T) {
	failed := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, errors.New("402 payment required"))
	script := &scriptedBench{results: []error{failed}}
	var done []benchRun
	stop := runBench(context.Background(), script.plan(jobsOf(3, perceptionOn), 20, 0), &done)
	if stop == "" || len(script.runs) != 1 || len(script.waited) != 0 || len(done) != 1 {
		t.Fatalf("stop %q runs %v waited %v", stop, script.runs, script.waited)
	}
}

func TestTheBenchRefusesAModelTheCatalogSaysCannotSee(t *testing.T) {
	script := &scriptedBench{refusals: map[string]error{"m": errors.New("does not see images")}}
	var done []benchRun
	if stop := runBench(context.Background(), script.plan(jobsOf(1, perceptionOn), 20, 0), &done); stop == "" || len(script.runs) != 0 {
		t.Fatalf("a blind model ran: %q", stop)
	}
}

func TestSpentIsTheSumOfEveryCountedResultsFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	if err := os.WriteFile(a, []byte(`[{"cost_usd":0.5},{"cost_usd":0.25}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`[{"case_id":"x","cost_usd":0.125}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	total, err := spentIn([]string{a, b, filepath.Join(dir, "missing.json")})
	if err != nil || total != 0.875 {
		t.Fatalf("total %v err %v", total, err)
	}
}

func TestALibraryPhotoReachesThePromptAsAMediaIDAndNeverAsAPicture(t *testing.T) {
	inputs, dir := benchFixture(t)
	inputs.Briefs[0].Photos = []benchPhoto{{MediaID: "photo-latte.jpg", Subject: "iced latte glass on white"}}
	jobs, err := benchJobs(inputs, dir, []string{trackBrief}, []string{"m"}, []string{perceptionOn}, nil)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
	if !strings.Contains(jobs[0].Prompt, "media_id: photo-latte.jpg") || !strings.Contains(jobs[0].Prompt, "iced latte") || jobs[0].Image != "" {
		t.Fatalf("prompt %q image %q", jobs[0].Prompt, jobs[0].Image)
	}
}

func TestALaterRoundRunsTheSameItemModelAndArmAgain(t *testing.T) {
	script := &scriptedBench{}
	done := []benchRun{{Track: trackBrief, Item: "b0", Model: "m", Perception: perceptionOn}}
	jobs := jobsOf(1, perceptionOn)
	jobs[0].Label = "r2"
	if stop := runBench(context.Background(), script.plan(jobs, 20, 0), &done); stop != "" {
		t.Fatal(stop)
	}
	if len(script.runs) != 1 || len(done) != 2 {
		t.Fatalf("runs %v done %d", script.runs, len(done))
	}
}
