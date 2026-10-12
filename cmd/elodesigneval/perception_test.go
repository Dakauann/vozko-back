package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/ai"
)

type scriptedAI struct {
	ai.Service
	replies []error
	inputs  []ai.GenerateInput
	cost    int64
}

func (s *scriptedAI) Generate(_ context.Context, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	s.inputs = append(s.inputs, input)
	if len(s.replies) > 0 {
		err := s.replies[0]
		s.replies = s.replies[1:]
		if err != nil {
			return nil, err
		}
	}
	return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: `{"defects":[]}`}, Usage: ai.Usage{PromptTokens: 10, CompletionTokens: 5, ProviderCostMicros: s.cost}}, nil
}

func perceptionJobs(n int) perceptionInputs {
	in := perceptionInputs{System: "revise"}
	for i := 0; i < n; i++ {
		in.Jobs = append(in.Jobs, perceptionJob{CaseID: string(rune('a' + i)), Arm: "base", Text: "contorno", Image: "data:image/jpeg;base64,AA=="})
	}
	return in
}

func TestThePerceptionEvalSendsEachCaseOnceThroughTheServiceAndRecordsTheProviderCost(t *testing.T) {
	service := &scriptedAI{cost: 12000}
	var done []perceptionResult
	stop := runPerception(context.Background(), service, perceptionJobs(2), perceptionPlan{Models: []string{"m"}, Cap: 10, Wait: func(time.Duration) {}, Save: func([]perceptionResult) error { return nil }}, &done)
	if stop != "" || len(done) != 2 || len(service.inputs) != 2 {
		t.Fatalf("stop %q, results %d, calls %d", stop, len(done), len(service.inputs))
	}
	if done[0].CostUSD != 0.012 || done[0].Model != "m" || service.inputs[0].Messages[0].Images[0] == "" || service.inputs[0].SystemPrompt != "revise" {
		t.Fatalf("result %+v input %+v", done[0], service.inputs[0])
	}
	again := runPerception(context.Background(), service, perceptionJobs(2), perceptionPlan{Models: []string{"m"}, Cap: 10, Wait: func(time.Duration) {}, Save: func([]perceptionResult) error { return nil }}, &done)
	if again != "" || len(service.inputs) != 2 {
		t.Fatalf("a finished case was sent again: %q, %d calls", again, len(service.inputs))
	}
}

func TestThePerceptionEvalNamesTheEvalWorkspaceAndTakesTheCostTheAdapterReported(t *testing.T) {
	service := &scriptedAI{}
	var done []perceptionResult
	plan := perceptionPlan{Models: []string{"m"}, Cap: 10, Wait: func(time.Duration) {}, Save: func([]perceptionResult) error { return nil }, Reported: func() int64 { return 30000 }}
	if stop := runPerception(context.Background(), service, perceptionJobs(1), plan, &done); stop != "" {
		t.Fatal(stop)
	}
	if service.inputs[0].WorkspaceID != evalWorkspace || service.inputs[0].BillingReference == "" || done[0].CostUSD != 0.03 {
		t.Fatalf("input %+v result %+v", service.inputs[0], done[0])
	}
}

func TestThePerceptionEvalStopsBeforeTheCapCountingWhatWasSpentBefore(t *testing.T) {
	service := &scriptedAI{cost: 400000}
	var done []perceptionResult
	stop := runPerception(context.Background(), service, perceptionJobs(5), perceptionPlan{Models: []string{"m"}, Cap: 10, Spent: 9, Wait: func(time.Duration) {}, Save: func([]perceptionResult) error { return nil }}, &done)
	if stop == "" || len(done) >= 5 {
		t.Fatalf("the cap did not stop the run: %q, %d results", stop, len(done))
	}
}

func TestThePerceptionEvalBacksOffOnceWhenTheProviderIsBusyAndThenStops(t *testing.T) {
	busy := &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", RetryAfter: 120 * time.Second, Err: errors.New("402")}
	service := &scriptedAI{replies: []error{busy, busy}}
	var waited []time.Duration
	var done []perceptionResult
	stop := runPerception(context.Background(), service, perceptionJobs(3), perceptionPlan{Models: []string{"m"}, Cap: 10, Wait: func(d time.Duration) { waited = append(waited, d) }, Save: func([]perceptionResult) error { return nil }}, &done)
	if stop == "" || len(waited) != 1 || waited[0] < 120*time.Second || len(service.inputs) != 2 || len(done) != 0 {
		t.Fatalf("stop %q waited %v calls %d results %d", stop, waited, len(service.inputs), len(done))
	}
}
