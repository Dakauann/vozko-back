package agentloop

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/ai"
)

const filledSummary = "## Objetivo\n- marcador retrô\n\n## Próximo passo\n1. montar o mar"

type checkpointDriver struct {
	*fakeDriver
	saved []string
	err   error
}

func (d *checkpointDriver) SaveCheckpoint(_ string, text string) error {
	d.saved = append(d.saved, text)
	return d.err
}

func full(prompt int) *ai.Usage { return &ai.Usage{PromptTokens: prompt, TotalTokens: prompt} }

func compactingConfig() Config {
	return Config{FinishToolName: "finish", IterateUntilDone: true, NoProgressStop: 100, ModelLimits: ai.ModelInfo{ContextLength: 40_000}}
}

func TestTheCeilingLeavesTheLargerOfTenPercentOrTheMinimumReserve(t *testing.T) {
	cases := map[int64]int{40_000: 24_000, 200_000: 180_000, 1_000_000: 900_000, 20_000: 18_000}
	for window, want := range cases {
		got, ok := Config{ModelLimits: ai.ModelInfo{ContextLength: window}}.compactionCeiling()
		if !ok || got != want {
			t.Errorf("window %d: ceiling %d, want %d", window, got, want)
		}
	}
	if _, ok := (Config{}).compactionCeiling(); ok {
		t.Error("an unknown window never compacts")
	}
}

func TestAFullContextIsSummarizedIntoACheckpointAndTheReplyGoesOn(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("read")}, usage: full(25_000)},
		{fullText: filledSummary},
		{fullText: "pronto"},
	}}
	drv := &checkpointDriver{fakeDriver: reading()}
	out, cp, sess := runWith(t, prov, drv, compactingConfig())
	if out.Kind != OutcomeIdle || out.Halt != nil {
		t.Fatalf("outcome %+v", out)
	}
	summaryCall := prov.inputs[1]
	last := summaryCall.Messages[len(summaryCall.Messages)-1]
	if summaryCall.ToolChoice != "none" || !strings.Contains(last.Content, "resumo estruturado") {
		t.Fatalf("the summary call must ask for the structured summary without tools: %+v", last)
	}
	after := prov.inputs[2].Messages
	if !isCheckpoint(after[0]) || !strings.Contains(after[0].Content, "marcador retrô") {
		t.Fatalf("the next call must open with the checkpoint: %+v", after[0])
	}
	if len(drv.saved) != 1 || drv.saved[0] != after[0].Content {
		t.Fatalf("the checkpoint must be saved for the next replies, saved %d", len(drv.saved))
	}
	if len(sess.Turn) != 1 || sess.Turn[0].Content != "pronto" {
		t.Fatalf("the reply transcript restarts after the checkpoint, got %+v", sess.Turn)
	}
	if cp.count(EventAssistantDone) != 2 {
		t.Fatalf("the summary call must stay silent, got %d answer events", cp.count(EventAssistantDone))
	}
}

func TestTheNewestExchangesStayVerbatimInsideTheCheckpoint(t *testing.T) {
	cfg := compactingConfig()
	cfg.CompactKeepTokens = 1_000
	prov := &fakeAI{turns: []aiTurn{{fullText: filledSummary}, {fullText: "pronto"}}}
	sess := &Session{History: []ai.Message{
		{Role: ai.RoleUser, Content: "primeiro pedido"},
		{Role: ai.RoleAssistant, Content: "feito " + strings.Repeat("antigo ", 15_000)},
		{Role: ai.RoleUser, Content: "segundo pedido"},
		{Role: ai.RoleAssistant, Content: "também feito"},
	}}
	e := Engine{AI: prov}
	e.Run(t.Context(), (&capture{}).emit, &checkpointDriver{fakeDriver: reading()}, cfg, sess, "terceiro")
	older := prov.inputs[0].Messages
	if !strings.Contains(older[0].Content, "primeiro pedido") || containsText(older, "segundo pedido") {
		t.Fatalf("only the older exchange goes to the summary, got %+v", older)
	}
	checkpoint := prov.inputs[1].Messages[0].Content
	if !strings.Contains(checkpoint, "<recent-context>") || !strings.Contains(checkpoint, "[Usuário]: segundo pedido") || !strings.Contains(checkpoint, "[Assistente]: também feito") {
		t.Fatalf("the recent exchanges must stay as text in the checkpoint: %s", checkpoint)
	}
}

func TestASummaryThatSkipsTheTemplateGetsOneReminder(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("read")}, usage: full(25_000)},
		{fullText: "resumo solto"},
		{fullText: filledSummary},
		{fullText: "pronto"},
	}}
	out, _, _ := runWith(t, prov, &checkpointDriver{fakeDriver: reading()}, compactingConfig())
	nudge := prov.inputs[2].Messages[len(prov.inputs[2].Messages)-1]
	if out.Halt != nil || !strings.Contains(nudge.Content, "títulos exatos") {
		t.Fatalf("outcome %+v, reminder %q", out, nudge.Content)
	}
}

func TestACompactionThatNeverFillsTheTemplateStopsTheReply(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("read")}, usage: full(25_000)},
		{fullText: "resumo solto"},
		{fullText: "de novo solto"},
	}}
	drv := &checkpointDriver{fakeDriver: reading()}
	out, _, _ := runWith(t, prov, drv, compactingConfig())
	if !errors.Is(out.Halt, ErrCompactionFailed) || len(drv.saved) != 0 {
		t.Fatalf("outcome %+v saved %d, want the reply stopped and nothing saved", out, len(drv.saved))
	}
}

func TestACheckpointThatCannotBeSavedStopsTheReply(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}, usage: full(25_000)}, {fullText: filledSummary}, {fullText: "pronto"}}}
	out, _, _ := runWith(t, prov, &checkpointDriver{fakeDriver: reading(), err: errors.New("db down")}, compactingConfig())
	if !errors.Is(out.Halt, ErrCompactionFailed) {
		t.Fatalf("outcome %+v, want the reply stopped when the checkpoint is not saved", out)
	}
}

func TestAContextTheProviderRejectsAsTooLongIsCompactedAndRetried(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{errEvent: fmt.Errorf("%w: prompt is too long", ai.ErrContextOverflow)},
		{fullText: filledSummary},
		{fullText: "pronto"},
	}}
	out, _, sess := runWith(t, prov, &checkpointDriver{fakeDriver: reading()}, compactingConfig())
	if out.Halt != nil || sess.History[len(sess.History)-1].Content != "pronto" {
		t.Fatalf("outcome %+v, want the reply to go on after compacting", out)
	}
}

func TestAnUnknownWindowNeverCompacts(t *testing.T) {
	cfg := compactingConfig()
	cfg.ModelLimits = ai.ModelInfo{}
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}, usage: full(900_000)}, {fullText: "pronto"}}}
	runWith(t, prov, &checkpointDriver{fakeDriver: reading()}, cfg)
	if len(prov.inputs) != 2 {
		t.Fatalf("%d calls, want no summary call", len(prov.inputs))
	}
}

func TestASecondCompactionUpdatesTheCheckpoint(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}, usage: full(25_000)}, {fullText: filledSummary}, {fullText: "pronto"}}}
	sess := &Session{History: []ai.Message{{Role: ai.RoleUser, Content: checkpointText("## Objetivo\n- antes", "")}}}
	e := Engine{AI: prov}
	e.Run(t.Context(), (&capture{}).emit, &checkpointDriver{fakeDriver: reading()}, compactingConfig(), sess, "continue")
	prompt := prov.inputs[1].Messages[len(prov.inputs[1].Messages)-1].Content
	if !strings.Contains(prompt, "Atualize o resumo anterior") {
		t.Fatalf("an existing checkpoint must be updated, got %q", prompt)
	}
}

func runWith(t *testing.T, prov *fakeAI, drv Driver, cfg Config) (Outcome, *capture, *Session) {
	t.Helper()
	e := Engine{AI: prov}
	cp := &capture{}
	sess := &Session{}
	out := e.Run(t.Context(), cp.emit, drv, cfg, sess, "faça X")
	return out, cp, sess
}

func containsText(msgs []ai.Message, text string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}
