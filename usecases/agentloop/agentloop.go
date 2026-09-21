package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

type Emit func(eventType string, payload interface{})

const (
	EventIteration      = "iteration"
	EventAssistantDelta = "assistant_delta"
	EventReasoningDelta = "reasoning_delta"
	EventReasoningDone  = "reasoning_done"
	EventAssistantDone  = "assistant_done"
	EventTool           = "tool"
)

type Driver interface {
	Model() string
	SystemPrompt() string
	Tools() []tools.Definition
	Reground(iter, maxIter, noMutationStreak int) string
	Dispatch(ctx context.Context, call ai.ToolCall, emit Emit) StepResult
	FinishVerdict(call ai.ToolCall) FinishResult
	Refresh()
	AfterTurn(emit Emit)
	Progress() Progress
}

type StepResult struct {
	Result    string
	Mutated   bool
	Pause     *Pause
	Signature string
}

type Pause struct {
	Reason  string
	Payload interface{}
}

type FinishResult struct {
	Honored      bool
	Summary      string
	Result       string
	EventSummary string
	PendingWork  int
}

type Progress struct {
	StateHash         string
	BlockingSignature string
	Valid             bool
}

type Config struct {
	WorkspaceID        string
	Temperature        float32
	MaxTokensPerGen    int
	ReasoningMaxTokens int
	MaxIterations      int
	NoProgressStop     int
	RepeatedTurnStop   int
	RepairBudget       int
	SessionTokenBudget int
	EmptyTurnRetries   int
	MaxHistoryMsgs     int
	FinishToolName     string
	LogPrefix          string
}

func (c Config) withDefaults() Config {
	if c.MaxIterations <= 0 {
		c.MaxIterations = 30
	}
	if c.NoProgressStop <= 0 {
		c.NoProgressStop = 5
	}
	if c.RepeatedTurnStop <= 0 {
		c.RepeatedTurnStop = 3
	}
	if c.RepairBudget <= 0 {
		c.RepairBudget = 3
	}
	if c.EmptyTurnRetries <= 0 {
		c.EmptyTurnRetries = 2
	}
	if c.MaxHistoryMsgs <= 0 {
		c.MaxHistoryMsgs = 80
	}
	if c.MaxTokensPerGen <= 0 {
		c.MaxTokensPerGen = 24000
	}
	if c.ReasoningMaxTokens <= 0 {
		c.ReasoningMaxTokens = 10000
	}
	if c.FinishToolName == "" {
		c.FinishToolName = "finish"
	}
	return c
}

type Session struct {
	History    []ai.Message
	TokensUsed int
}

type OutcomeKind int

const (
	OutcomeDone OutcomeKind = iota
	OutcomeIdle
	OutcomePaused
)

type Outcome struct {
	Kind    OutcomeKind
	Valid   bool
	Summary string
	Pause   *Pause
}

type Engine struct {
	AI ai.Service
}

const (
	reasonMaxIterations   = "número máximo de iterações atingido"
	reasonTokenBudget     = "limite de tokens da sessão atingido"
	reasonNoProgressState = "sem progresso, o grafo não mudou nas últimas iterações"
	reasonChurn           = "sem progresso, o mesmo conjunto de problemas persiste"
	reasonRepairExhausted = "orçamento de reparo esgotado"
	reasonRepeatedTurn    = "sem progresso, as mesmas ações se repetiram sem convergir"
	reasonEmptyTurn       = "o modelo não produziu nenhuma ação, possível truncamento pelo limite de tokens de raciocínio do modelo (tente novamente ou troque de modelo)"
	finishIgnoredMsg      = "finish IGNORADO: você fez mutações neste turno, chame finish sozinho, sem outras ferramentas."
	providerErrPrefix     = "erro do provedor de IA: "
	reasonCancelled       = "cancelado"
	reasonTimeout         = "tempo limite da sessão atingido, o modelo demorou demais para responder (tente novamente ou troque para um modelo mais rápido)"
)

func sessionEndReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return reasonTimeout
	}
	return reasonCancelled
}

type toolEvent struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Ok      bool   `json:"ok"`
}

type iterationPayload struct {
	N           int `json:"n"`
	Max         int `json:"max"`
	TokensUsed  int `json:"tokensUsed"`
	TokenBudget int `json:"tokenBudget"`
}

func (e *Engine) Run(ctx context.Context, emit Emit, drv Driver, cfg Config, sess *Session, prompt string) Outcome {
	cfg = cfg.withDefaults()

	drv.Refresh()
	prog := drv.Progress()

	prevSig := ""
	sameCount := 0
	budget := cfg.RepairBudget
	lastPending := -1
	prevHash := prog.StateHash
	unchangedTurns := 0
	noMutationStreak := 0
	truncStreak := 0
	prevTurnSig := ""
	repeatedTurns := 0

	sess.History = append(sess.History, ai.Message{Role: ai.RoleUser, Content: "PEDIDO DO USUÁRIO:\n" + prompt})
	sess.History = trimHistory(sess.History, cfg.MaxHistoryMsgs)

	for iter := 1; iter <= cfg.MaxIterations; iter++ {
		if ctx.Err() != nil {
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: sessionEndReason(ctx.Err())}
		}
		if cfg.SessionTokenBudget > 0 && sess.TokensUsed >= cfg.SessionTokenBudget {
			return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonTokenBudget}
		}

		emit(EventIteration, iterationPayload{N: iter, Max: cfg.MaxIterations, TokensUsed: sess.TokensUsed, TokenBudget: cfg.SessionTokenBudget})

		msgs := append(append([]ai.Message(nil), sess.History...),
			ai.Message{Role: ai.RoleUser, Content: drv.Reground(iter, cfg.MaxIterations, noMutationStreak)})

		out, err := e.streamGenerate(ctx, emit, ai.GenerateInput{
			Model:              drv.Model(),
			Temperature:        cfg.Temperature,
			MaxTokens:          cfg.MaxTokensPerGen,
			ReasoningMaxTokens: cfg.ReasoningMaxTokens,
			SystemPrompt:       drv.SystemPrompt(),
			Messages:           msgs,
			Tools:              drv.Tools(),
			ToolExecutionMode:  ai.ToolExecutionModeNone,
			WorkspaceID:        cfg.WorkspaceID,
		})
		if err != nil {
			if ctx.Err() != nil {
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: sessionEndReason(ctx.Err())}
			}
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: providerErrPrefix + err.Error()}
		}
		sess.TokensUsed += out.Usage.TotalTokens

		calls := make([]ai.ToolCall, len(out.ToolCalls))
		for i, tc := range out.ToolCalls {
			if strings.TrimSpace(tc.ID) == "" {
				tc.ID = fmt.Sprintf("call_%d_%d", iter, i)
			}
			calls[i] = tc
		}

		content := strings.TrimSpace(out.Message.Content)
		emit(EventAssistantDone, map[string]int{"tools": len(calls)})
		if cfg.LogPrefix != "" {
			names := make([]string, 0, len(calls))
			for _, tc := range calls {
				names = append(names, tc.Name)
			}
			log.Printf("%s iter=%d/%d tokens=%d finish=%s toolcalls=[%s]",
				cfg.LogPrefix, iter, cfg.MaxIterations, sess.TokensUsed, out.FinishReason, strings.Join(names, ","))
		}

		if len(calls) == 0 {
			if content == "" || out.FinishReason == "length" {
				truncStreak++
				if truncStreak <= cfg.EmptyTurnRetries {
					continue
				}
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonEmptyTurn}
			}
			sess.History = append(sess.History, ai.Message{Role: ai.RoleAssistant, Content: content})
			sess.History = trimHistory(sess.History, cfg.MaxHistoryMsgs)
			return Outcome{Kind: OutcomeIdle, Valid: prog.Valid}
		}
		truncStreak = 0

		results := make([]string, len(calls))
		finishIdx := -1
		var finishCall ai.ToolCall
		mutated := false
		turnSignature := make([]string, 0, len(calls))
		for i := range calls {
			tc := calls[i]
			if tc.Name == cfg.FinishToolName {
				finishIdx = i
				finishCall = tc
				continue
			}
			step := drv.Dispatch(ctx, tc, emit)
			results[i] = step.Result
			if step.Mutated {
				mutated = true
			}
			if step.Signature != "" {
				turnSignature = append(turnSignature, step.Signature)
			}
			if step.Pause != nil {
				recordTurn(sess, cfg.MaxHistoryMsgs, out.Message.Content, calls[:i+1], results[:i+1])
				return Outcome{Kind: OutcomePaused, Valid: prog.Valid, Summary: step.Pause.Reason, Pause: step.Pause}
			}
		}

		drv.AfterTurn(emit)
		prog = drv.Progress()
		if mutated {
			noMutationStreak = 0
		} else {
			noMutationStreak++
		}

		finishHonored := false
		repairExhausted := false
		finishSummary := ""
		if finishIdx >= 0 {
			switch {
			case mutated:
				results[finishIdx] = finishIgnoredMsg
			default:
				fr := drv.FinishVerdict(finishCall)
				results[finishIdx] = fr.Result
				if fr.Honored {
					finishHonored = true
					finishSummary = fr.Summary
				} else {
					emit(EventTool, toolEvent{Name: cfg.FinishToolName, Summary: fr.EventSummary, Ok: false})
					if lastPending >= 0 && fr.PendingWork >= lastPending {
						budget--
					}
					lastPending = fr.PendingWork
					repairExhausted = budget < 0
				}
			}
		}

		recordTurn(sess, cfg.MaxHistoryMsgs, out.Message.Content, calls, results)

		if finishHonored {
			emit(EventTool, toolEvent{Name: cfg.FinishToolName, Summary: finishSummary, Ok: true})
			return Outcome{Kind: OutcomeDone, Valid: true, Summary: finishSummary}
		}
		if repairExhausted {
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonRepairExhausted}
		}

		if prog.StateHash != "" && prog.StateHash == prevHash {
			unchangedTurns++
			if unchangedTurns >= cfg.NoProgressStop {
				return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonNoProgressState}
			}
		} else {
			unchangedTurns = 0
		}
		prevHash = prog.StateHash

		sig := prog.BlockingSignature
		if sig != "" && sig == prevSig {
			sameCount++
			if sameCount >= cfg.NoProgressStop {
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonChurn}
			}
		} else {
			sameCount = 0
		}
		prevSig = sig

		sort.Strings(turnSignature)
		turnSig := strings.Join(turnSignature, ",")
		if turnSig != "" && turnSig == prevTurnSig {
			repeatedTurns++
			if repeatedTurns >= cfg.RepeatedTurnStop {
				return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonRepeatedTurn}
			}
		} else {
			repeatedTurns = 0
		}
		prevTurnSig = turnSig
	}

	return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonMaxIterations}
}

func (e *Engine) streamGenerate(ctx context.Context, emit Emit, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	ch, err := e.AI.GenerateStream(ctx, input)
	if err != nil {
		return nil, err
	}
	out := &ai.GenerateOutput{}
	var content, pending, reasoning strings.Builder
	reasoningStreamed := false
	flush := func() {
		if pending.Len() == 0 {
			return
		}
		emit(EventAssistantDelta, map[string]string{"text": pending.String()})
		pending.Reset()
	}
	flushReasoning := func() {
		if reasoning.Len() == 0 {
			return
		}
		emit(EventReasoningDelta, map[string]string{"text": reasoning.String()})
		reasoning.Reset()
	}
	endReasoning := func() {
		if !reasoningStreamed {
			return
		}
		flushReasoning()
		emit(EventReasoningDone, nil)
		reasoningStreamed = false
	}
	for ev := range ch {
		switch ev.Type {
		case ai.StreamEventReasoning:
			reasoningStreamed = true
			reasoning.WriteString(ev.Token)
			if reasoning.Len() >= 24 || strings.ContainsAny(ev.Token, ".\n!?") {
				flushReasoning()
			}
		case ai.StreamEventToken:
			endReasoning()
			content.WriteString(ev.Token)
			pending.WriteString(ev.Token)
			if pending.Len() >= 10 || strings.ContainsAny(ev.Token, ".\n!?") {
				flush()
			}
		case ai.StreamEventDone:
			out.ToolCalls = ev.AllToolCalls
			out.FinishReason = ev.FinishReason
			if ev.Usage != nil {
				out.Usage = *ev.Usage
			}
			if content.Len() == 0 {
				content.WriteString(ev.FullText)
			}
		case ai.StreamEventError:
			flush()
			endReasoning()
			return nil, ev.Error
		}
	}
	flush()
	endReasoning()
	out.Message = ai.Message{Role: ai.RoleAssistant, Content: content.String()}
	return out, nil
}

func recordTurn(sess *Session, maxHistory int, content string, calls []ai.ToolCall, results []string) {
	sess.History = append(sess.History, ai.Message{Role: ai.RoleAssistant, Content: content, ToolCalls: calls})
	for i, tc := range calls {
		r := ""
		if i < len(results) {
			r = results[i]
		}
		if strings.TrimSpace(r) == "" {
			r = "ok"
		}
		sess.History = append(sess.History, ai.Message{Role: ai.RoleTool, ToolCallID: tc.ID, Content: r})
	}
	sess.History = trimHistory(sess.History, maxHistory)
}

func trimHistory(h []ai.Message, max int) []ai.Message {
	if len(h) <= max {
		return h
	}
	start := len(h) - max
	for start < len(h) && h[start].Role == ai.RoleTool {
		start++
	}
	if start <= 1 {
		return h[start:]
	}
	trimmed := make([]ai.Message, 0, len(h)-start+1)
	if h[0].Role == ai.RoleUser {
		trimmed = append(trimmed, h[0])
	}
	trimmed = append(trimmed, h[start:]...)
	return trimmed
}
