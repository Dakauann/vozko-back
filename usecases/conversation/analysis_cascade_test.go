package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/livedecision"
	"vozko/domain/shared"
	toolsdomain "vozko/domain/tools"
	livedecisions_usecase "vozko/usecases/livedecisions"
	tools_usecase "vozko/usecases/tools"
)

type cascadeStub struct {
	stageNeedsReview bool
	gate             livedecision.QuietGate
	asked            []string
}

func (c *cascadeStub) StageNeedsReview(_ context.Context, t livedecisions_usecase.Trigger) bool {
	c.asked = append(c.asked, "stage:"+t.EntryID)
	return c.stageNeedsReview
}

func (c *cascadeStub) QuietGate(_ context.Context, t livedecisions_usecase.Trigger, wantMemory, wantDeals bool, memory, deals string) livedecision.QuietGate {
	c.asked = append(c.asked, "gate:"+t.EntryID)
	return livedecision.QuietGate{NeedsMemory: wantMemory && c.gate.NeedsMemory, NeedsDeals: wantDeals && c.gate.NeedsDeals}
}

type describingStageTool struct{ toolsdomain.Handler }

func (describingStageTool) Definition() toolsdomain.Definition {
	return toolsdomain.Definition{Name: tools_usecase.ManageEntryStageToolName}
}

func cascadeJob(cascade *cascadeStub) (*analysisDebounceJob, *generateRecorder) {
	subject := instagramSubject()
	subject.EnableAutoStaging = true
	job, recorder := dealJob(subject, &dealSettingsStub{pipeline: "deals"})
	job.toolRegistry.(registryStub).handlers[tools_usecase.ManageEntryStageToolName] = describingStageTool{}
	job.SetQuietCascade(cascade)
	return job, recorder
}

func toolNames(input []toolsdomain.Definition) []string {
	names := make([]string, 0, len(input))
	for _, d := range input {
		names = append(names, d.Name)
	}
	return names
}

func TestQuietWindowSkipsTheLLMWhenEveryGateIsSettled(t *testing.T) {
	cascade := &cascadeStub{}
	job, recorder := cascadeJob(cascade)
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	if len(recorder.inputs) != 0 {
		t.Fatalf("a settled stage and a confident 'nothing changed' need no LLM call; got tools %v", toolNames(recorder.inputs[0].Tools))
	}
	if len(cascade.asked) != 2 || cascade.asked[0] != "stage:entry-1" || cascade.asked[1] != "gate:entry-1" {
		t.Fatalf("asked = %v", cascade.asked)
	}
}

func calledTools(recorder *generateRecorder) [][]string {
	calls := make([][]string, 0, len(recorder.inputs))
	for _, input := range recorder.inputs {
		calls = append(calls, toolNames(input.Tools))
	}
	return calls
}

func TestEachDecisionThatStillNeedsTheLLMGetsItsOwnCall(t *testing.T) {
	cascade := &cascadeStub{stageNeedsReview: true, gate: livedecision.QuietGate{NeedsDeals: true}}
	job, recorder := cascadeJob(cascade)
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	calls := calledTools(recorder)
	if len(calls) != 2 || len(calls[0]) != 1 || calls[0][0] != tools_usecase.ManageEntryStageToolName ||
		len(calls[1]) != 1 || calls[1][0] != tools_usecase.AutoManageOpportunityToolName {
		t.Fatalf("bundled decisions pull each other off course; calls = %v", calls)
	}
	stage, deals := recorder.inputs[0], recorder.inputs[1]
	if !strings.Contains(stage.SystemPrompt, "TAGS DISPONÍVEIS") || strings.Contains(stage.SystemPrompt, "auto_manage_opportunity") {
		t.Fatal("the stage call must carry only the stage task")
	}
	if !strings.Contains(deals.SystemPrompt, "auto_manage_opportunity") || strings.Contains(deals.SystemPrompt, "TAGS DISPONÍVEIS") {
		t.Fatal("the opportunity call must carry only the opportunity task")
	}
	if stage.Messages[0].Content != autoTagInstruction || deals.Messages[0].Content != autoDealInstruction {
		t.Fatal("each call carries its own instruction")
	}
}

func TestAFailedDecisionStopsTheRunSoItIsRetried(t *testing.T) {
	cascade := &cascadeStub{stageNeedsReview: true, gate: livedecision.QuietGate{NeedsDeals: true}}
	job, recorder := cascadeJob(cascade)
	recorder.err = errors.New("provider down")
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err == nil {
		t.Fatal("a failed call must surface so the debounce keeps the entry for a retry")
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("calls after a failure = %d", len(recorder.inputs))
	}
}

func TestWithoutACascadeTheQuietWindowWorksAsBefore(t *testing.T) {
	subject := instagramSubject()
	subject.EnableAutoStaging = true
	job, recorder := dealJob(subject, &dealSettingsStub{pipeline: "deals"})
	job.toolRegistry.(registryStub).handlers[tools_usecase.ManageEntryStageToolName] = describingStageTool{}
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	if calls := calledTools(recorder); len(calls) != 2 || len(calls[0]) != 1 || len(calls[1]) != 1 {
		t.Fatalf("every enabled decision runs in its own call: %v", calls)
	}
}
