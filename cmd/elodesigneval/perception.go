package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vozko/domain/ai"
)

const (
	perceptionHardCap       = 10.0
	perceptionFirstGuessUSD = 0.05
	perceptionSafety        = 1.5
	perceptionMinBackOff    = 120 * time.Second
	perceptionMaxTokens     = 1500
	perceptionReasonTokens  = 6000
)

type perceptionJob struct {
	CaseID string `json:"case_id"`
	Arm    string `json:"arm"`
	Text   string `json:"text"`
	Image  string `json:"image"`
}

type perceptionInputs struct {
	System string          `json:"system"`
	Jobs   []perceptionJob `json:"jobs"`
}

type perceptionResult struct {
	CaseID           string  `json:"case_id"`
	Arm              string  `json:"arm"`
	Model            string  `json:"model"`
	Reasoning        string  `json:"reasoning,omitempty"`
	Text             string  `json:"text"`
	CostUSD          float64 `json:"cost_usd"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
}

type perceptionPlan struct {
	Models    []string
	Reasoning ai.ReasoningLevel
	Cap       float64
	Spent     float64
	Wait      func(time.Duration)
	Save      func([]perceptionResult) error
	Reported  func() int64
}

func (p perceptionPlan) reportedMicros() int64 {
	if p.Reported == nil {
		return 0
	}
	return p.Reported()
}

func spentOn(results []perceptionResult) float64 {
	total := 0.0
	for _, r := range results {
		total += r.CostUSD
	}
	return total
}

func finished(results []perceptionResult, job perceptionJob, model string, reasoning ai.ReasoningLevel) bool {
	for _, r := range results {
		if r.CaseID == job.CaseID && r.Arm == job.Arm && r.Model == model && r.Reasoning == string(reasoning) {
			return true
		}
	}
	return false
}

func perceptionInput(system string, job perceptionJob, model string, reasoning ai.ReasoningLevel) ai.GenerateInput {
	input := ai.GenerateInput{
		Model:            model,
		SystemPrompt:     system,
		Messages:         []ai.Message{{Role: ai.RoleUser, Content: job.Text, Images: []string{job.Image}}},
		MaxTokens:        perceptionMaxTokens,
		ToolChoice:       "none",
		WorkspaceID:      evalWorkspace,
		BillingReference: "elo-perception-eval:" + job.CaseID + ":" + job.Arm,
	}
	if reasoning != "" {
		input.ReasoningEffort = reasoning
		input.MaxTokens = perceptionReasonTokens
	}
	return input
}

func runPerception(ctx context.Context, service ai.Service, inputs perceptionInputs, plan perceptionPlan, done *[]perceptionResult) string {
	dearest := perceptionFirstGuessUSD
	for _, job := range inputs.Jobs {
		for _, model := range plan.Models {
			if finished(*done, job, model, plan.Reasoning) {
				continue
			}
			spent := plan.Spent + spentOn(*done)
			if spent+dearest*perceptionSafety > plan.Cap {
				return fmt.Sprintf("stopped before the US$ %.2f cap (spent US$ %.4f)", plan.Cap, spent)
			}
			out, err := service.Generate(ctx, perceptionInput(inputs.System, job, model, plan.Reasoning))
			if wait, busy := ai.BusyRetryAfter(err); busy {
				plan.Wait(max(wait, perceptionMinBackOff))
				out, err = service.Generate(ctx, perceptionInput(inputs.System, job, model, plan.Reasoning))
				if errors.Is(err, ai.ErrProviderBusy) {
					return fmt.Sprintf("stopped: the provider is still busy after one back off (%v)", err)
				}
			}
			if err != nil {
				return fmt.Sprintf("stopped on %s %s %s: %v", job.CaseID, job.Arm, model, err)
			}
			cost := float64(max(plan.reportedMicros(), out.Usage.ProviderCostMicros)) / 1e6
			dearest = max(dearest, cost)
			*done = append(*done, perceptionResult{CaseID: job.CaseID, Arm: job.Arm, Model: model, Reasoning: string(plan.Reasoning), Text: out.Message.Content, CostUSD: cost, PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens})
			if err := plan.Save(*done); err != nil {
				return fmt.Sprintf("stopped: the results could not be saved: %v", err)
			}
		}
	}
	return ""
}

func readPerception(inputsPath, outPath string) (perceptionInputs, []perceptionResult, error) {
	var inputs perceptionInputs
	raw, err := os.ReadFile(inputsPath)
	if err != nil {
		return inputs, nil, err
	}
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return inputs, nil, err
	}
	var done []perceptionResult
	if previous, err := os.ReadFile(outPath); err == nil {
		if err := json.Unmarshal(previous, &done); err != nil {
			return inputs, nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return inputs, nil, err
	}
	return inputs, done, nil
}

func withPictures(inputs perceptionInputs, dir string) (perceptionInputs, error) {
	pictures := map[string]string{}
	for i, job := range inputs.Jobs {
		if _, ok := pictures[job.Image]; !ok {
			raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(job.Image)))
			if err != nil {
				return inputs, err
			}
			pictures[job.Image] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw)
		}
		inputs.Jobs[i].Image = pictures[job.Image]
	}
	return inputs, nil
}

func perceptionMain(service ai.Service, ledger *usageLedger, inputsPath, outPath string, models []string, reasoning ai.ReasoningLevel, spent, capUSD float64) int {
	var chosen []string
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			chosen = append(chosen, model)
		}
	}
	if len(chosen) == 0 || spent < 0 || capUSD <= 0 || capUSD > perceptionHardCap {
		fmt.Fprintf(os.Stderr, "the perception eval needs -models, -spent (what earlier runs cost) and a -cap up to US$ %.0f\n", perceptionHardCap)
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
	plan := perceptionPlan{Models: chosen, Reasoning: reasoning, Cap: capUSD, Spent: spent, Wait: time.Sleep, Save: func(results []perceptionResult) error { return writePerception(outPath, results) }, Reported: ledger.take}
	before := len(done)
	stop := runPerception(context.Background(), service, inputs, plan, &done)
	fmt.Printf("%d new calls, US$ %.4f in this file, US$ %.4f with earlier runs\n", len(done)-before, spentOn(done), spent+spentOn(done))
	if stop != "" {
		fmt.Println(stop)
	}
	return 0
}

func writePerception(outPath string, done []perceptionResult) error {
	raw, err := json.MarshalIndent(done, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, raw, 0o644)
}
