package text_refiner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/text_refiner"
	"vozko/domain/workspace/workspace_pricing"
)

type fakeAI struct {
	resp *ai.GenerateOutput
	err  error

	seen ai.GenerateInput
}

func (f *fakeAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	f.seen = in
	return f.resp, f.err
}

func (f *fakeAI) GenerateStream(context.Context, ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAI) GetAvaibleModels(context.Context) ([]string, error) { return nil, nil }
func (f *fakeAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return nil, nil
}

type fakePricer struct {
	price int64
	err   error
}

func (f *fakePricer) ResolveForWorkspace(string) ([]workspace_pricing.ResolvedPricingItem, error) {
	return nil, nil
}
func (f *fakePricer) PriceSTT(string, string, float64) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (f *fakePricer) PriceTTS(string, string, string, int) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (f *fakePricer) PriceTelephony(string, float64) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (f *fakePricer) PriceTelephonyChannel(ws string, dur float64, _ string) (workspace_pricing.PriceResult, error) {
	return f.PriceTelephony(ws, dur)
}
func (f *fakePricer) PriceWhatsApp(string, string) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (f *fakePricer) PriceLLM(string, string, int, int) (workspace_pricing.PriceResult, error) {
	if f.err != nil {
		return workspace_pricing.PriceResult{}, f.err
	}
	return workspace_pricing.PriceResult{CostMicros: f.price, PriceMicros: f.price, ProfitMicros: 0}, nil
}

type fakeChecker struct {
	sufficient bool
	err        error

	gotAmount int64
}

func (f *fakeChecker) HasSufficientBalance(_ string, amount int64) (bool, error) {
	f.gotAmount = amount
	return f.sufficient, f.err
}
func (f *fakeChecker) GetBalance(string) (int64, error) { return 0, nil }
func (f *fakeChecker) Invalidate(string)                {}
func (f *fakeChecker) InvalidateDebounced(string)       {}

func TestExecute_HappyPath(t *testing.T) {
	fAI := &fakeAI{resp: &ai.GenerateOutput{
		Message: ai.Message{Role: ai.RoleAssistant, Content: "the quick brown fox"},
		Usage:   ai.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
	}}
	uc := NewRefineTextUseCase(fAI, &fakePricer{price: 1000}, &fakeChecker{sufficient: true})

	out, err := uc.Execute(context.Background(), text_refiner.RefineInput{
		WorkspaceID:  "ws1",
		Instruction:  "fix grammar",
		OriginalText: "the quik brown fox",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.RefinedText != "the quick brown fox" {
		t.Errorf("refined = %q", out.RefinedText)
	}
	if len(out.Segments) == 0 {
		t.Errorf("expected segments, got none")
	}
	if fAI.seen.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID not forwarded to AI (got %q), billing event will be skipped", fAI.seen.WorkspaceID)
	}
	if fAI.seen.Model == "" {
		t.Errorf("default model not applied")
	}
}

func TestExecute_InsufficientBalance(t *testing.T) {
	uc := NewRefineTextUseCase(
		&fakeAI{resp: &ai.GenerateOutput{}},
		&fakePricer{price: 1_000_000},
		&fakeChecker{sufficient: false},
	)
	_, err := uc.Execute(context.Background(), text_refiner.RefineInput{
		WorkspaceID:  "ws1",
		Instruction:  "do it",
		OriginalText: "hello",
	})
	if !errors.Is(err, text_refiner.ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestExecute_ValidationErrors(t *testing.T) {
	uc := NewRefineTextUseCase(&fakeAI{}, &fakePricer{}, &fakeChecker{sufficient: true})

	cases := []struct {
		name string
		in   text_refiner.RefineInput
		want error
	}{
		{"missing text", text_refiner.RefineInput{WorkspaceID: "w", Instruction: "i"}, text_refiner.ErrTextRequired},
		{"missing instr", text_refiner.RefineInput{WorkspaceID: "w", OriginalText: "t"}, text_refiner.ErrInstructionRequired},
		{"text too long", text_refiner.RefineInput{WorkspaceID: "w", Instruction: "i", OriginalText: strings.Repeat("a", maxOriginalTextChars+1)}, text_refiner.ErrTextTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := uc.Execute(context.Background(), tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestExecute_SafetyMarginApplied(t *testing.T) {
	ch := &fakeChecker{sufficient: true}
	uc := NewRefineTextUseCase(
		&fakeAI{resp: &ai.GenerateOutput{Message: ai.Message{Content: "x"}}},
		&fakePricer{price: 1000},
		ch,
	)
	_, err := uc.Execute(context.Background(), text_refiner.RefineInput{
		WorkspaceID: "w", Instruction: "i", OriginalText: "t",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	if ch.gotAmount != 1250 {
		t.Errorf("expected 1250 micros with 25%% safety margin, got %d", ch.gotAmount)
	}
}

func TestExecute_StripsFences(t *testing.T) {
	fAI := &fakeAI{resp: &ai.GenerateOutput{
		Message: ai.Message{Content: "```\nrefined\n```"},
	}}
	uc := NewRefineTextUseCase(fAI, &fakePricer{price: 1}, &fakeChecker{sufficient: true})
	out, err := uc.Execute(context.Background(), text_refiner.RefineInput{
		WorkspaceID: "w", Instruction: "i", OriginalText: "t",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.RefinedText != "refined" {
		t.Errorf("fences not stripped: %q", out.RefinedText)
	}
}

func TestComputeSegments_Reconstructs(t *testing.T) {
	orig := "The quick brown fox jumps over the lazy dog."
	refined := "The quick red fox leaps over a lazy dog."
	segs := computeSegments(orig, refined)
	if len(segs) == 0 {
		t.Fatalf("no segments produced")
	}
	var rebuilt strings.Builder
	for _, s := range segs {
		if s.Op != text_refiner.DiffOpDelete {
			rebuilt.WriteString(s.Text)
		}
	}
	if rebuilt.String() != refined {
		t.Errorf("segments do not reconstruct refined text:\n got: %q\nwant: %q", rebuilt.String(), refined)
	}
}

func TestBuildUnifiedDiff_NoChangeEmpty(t *testing.T) {
	if d := buildUnifiedDiff("same\n", "same\n"); d != "" {
		t.Errorf("expected empty diff for identical text, got %q", d)
	}
}
