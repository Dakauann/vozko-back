package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

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
	Images    []string
	EndTurn   bool
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
	BillingReference   string
	SessionID          string
	AsOf               time.Time
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
	ModelLimits        ai.ModelInfo
	CompactAt          float64
	KeepRecent         int
	CostCeilingMicros  int64
	GraceInstruction   string
	KeepToolImages     int
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
	if c.CompactAt <= 0 || c.CompactAt >= 1 {
		c.CompactAt = 0.5
	}
	if c.KeepRecent <= 0 {
		c.KeepRecent = 8
	}
	if c.KeepToolImages <= 0 {
		c.KeepToolImages = 2
	}
	return c
}

const (
	regroundTail      = 1
	userRequestPrefix = "PEDIDO DO USUÁRIO:\n"
)

func UserRequest(prompt string) string {
	return userRequestPrefix + prompt
}

func (c Config) request(drv Driver, messages []ai.Message) ai.GenerateInput {
	return ai.GenerateInput{
		Model:              drv.Model(),
		Temperature:        c.Temperature,
		MaxTokens:          c.MaxTokensPerGen,
		ReasoningMaxTokens: c.ReasoningMaxTokens,
		SystemPrompt:       drv.SystemPrompt(),
		Messages:           messages,
		Tools:              drv.Tools(),
		ToolExecutionMode:  ai.ToolExecutionModeNone,
		WorkspaceID:        c.WorkspaceID,
		BillingReference:   c.BillingReference,
		SessionID:          c.SessionID,
		AsOf:               c.AsOf,
		VolatileTail:       regroundTail,
	}
}

func (c Config) costOf(u ai.Usage) int64 {
	return c.ModelLimits.CostMicros(u)
}

func (c Config) budgetSpent(sess *Session) bool {
	tokens := c.SessionTokenBudget > 0 && sess.TokensUsed >= c.SessionTokenBudget
	money := c.CostCeilingMicros > 0 && sess.CostMicros >= c.CostCeilingMicros
	return tokens || money
}

func (c Config) contextFull(promptTokens int) bool {
	window := c.ModelLimits.ContextLength
	return window > 0 && float64(promptTokens) >= c.CompactAt*float64(window)
}

type Session struct {
	History      []ai.Message
	PromptImages []string
	TokensUsed   int
	CostMicros   int64
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
	Halt    error
}

var (
	ErrSessionBudget   = errors.New("agentloop: session token budget exhausted")
	ErrOutputTruncated = errors.New("agentloop: the model output kept hitting its length limit")
	ErrProviderFailed  = errors.New("agentloop: the AI provider refused or failed the call")
)

type Guard interface {
	Admit(ctx context.Context) error
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
	reasonHalted          = "interrompido antes da próxima chamada ao modelo"
	reasonTimeout         = "tempo limite da sessão atingido, o modelo demorou demais para responder (tente novamente ou troque para um modelo mais rápido)"
	clearedToolResult     = "[resultado antigo removido para liberar espaço; consulte de novo se precisar]"
	clearableResultChars  = 200
	truncatedReplyNudge   = "Sua resposta anterior foi cortada pelo limite de saída antes de você agir. Planeje menos e aja em passos menores: uma cena ou um lote de até 8 operações por vez."
	truncatedCallResult   = "CHAMADA CORTADA: a resposta passou do limite de saída e os argumentos chegaram incompletos, então nada foi executado. Refaça em partes menores (por exemplo, lotes de até 12 operações)."
	toolImagesNote        = "[Imagens devolvidas pelas ferramentas acima]"
	toolImagesCleared     = toolImagesNote + " (removidas para liberar espaço; capture de novo se precisar)"
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
	if cfg.AsOf.IsZero() {
		cfg.AsOf = time.Now()
	}

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
	lastPromptTokens := 0

	sess.History = append(sess.History, ai.Message{Role: ai.RoleUser, Content: UserRequest(prompt), Images: sess.PromptImages})
	sess.History = trimHistory(sess.History, cfg.MaxHistoryMsgs)

	for iter := 1; iter <= cfg.MaxIterations; iter++ {
		if ctx.Err() != nil {
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: sessionEndReason(ctx.Err())}
		}
		if cfg.budgetSpent(sess) {
			return e.graceAnswer(ctx, emit, drv, cfg, sess, Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonTokenBudget, Halt: ErrSessionBudget})
		}
		if guard, guarded := drv.(Guard); guarded {
			if err := guard.Admit(ctx); err != nil {
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonHalted, Halt: err}
			}
		}

		emit(EventIteration, iterationPayload{N: iter, Max: cfg.MaxIterations, TokensUsed: sess.TokensUsed, TokenBudget: cfg.SessionTokenBudget})
		if cfg.contextFull(lastPromptTokens) {
			sess.History = clearOldToolResults(sess.History, cfg.KeepRecent)
		}

		msgs := append(append([]ai.Message(nil), sess.History...),
			ai.Message{Role: ai.RoleUser, Content: drv.Reground(iter, cfg.MaxIterations, noMutationStreak)})

		out, err := e.streamGenerate(ctx, emit, cfg.request(drv, msgs))
		if err != nil {
			if ctx.Err() != nil {
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: sessionEndReason(ctx.Err())}
			}
			log.Printf("%s %s%v", cfg.LogPrefix, providerErrPrefix, err)
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: providerErrPrefix + err.Error(), Halt: fmt.Errorf("%w: %w", ErrProviderFailed, err)}
		}
		sess.TokensUsed += out.Usage.TotalTokens
		sess.CostMicros += cfg.costOf(out.Usage)
		lastPromptTokens = out.Usage.PromptTokens

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
				if truncStreak > cfg.EmptyTurnRetries {
					return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonEmptyTurn, Halt: ErrOutputTruncated}
				}
				if out.FinishReason == "length" {
					sess.History = append(sess.History, ai.Message{Role: ai.RoleUser, Content: truncatedReplyNudge})
				}
				continue
			}
			sess.History = append(sess.History, ai.Message{Role: ai.RoleAssistant, Content: content})
			sess.History = trimHistory(sess.History, cfg.MaxHistoryMsgs)
			return Outcome{Kind: OutcomeIdle, Valid: prog.Valid}
		}
		if out.FinishReason == "length" {
			truncStreak++
			recordTurn(sess, cfg.MaxHistoryMsgs, out.Message.Content, calls, truncatedResults(len(calls)))
			if truncStreak > cfg.EmptyTurnRetries {
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonEmptyTurn, Halt: ErrOutputTruncated}
			}
			continue
		}
		truncStreak = 0

		results := make([]string, len(calls))
		finishIdx := -1
		var finishCall ai.ToolCall
		mutated := false
		endTurn := false
		var images []string
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
			images = append(images, step.Images...)
			endTurn = endTurn || step.EndTurn
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
		attachToolImages(sess, cfg, images)

		if finishHonored {
			emit(EventTool, toolEvent{Name: cfg.FinishToolName, Summary: finishSummary, Ok: true})
			return Outcome{Kind: OutcomeDone, Valid: true, Summary: finishSummary}
		}
		if endTurn {
			return Outcome{Kind: OutcomeIdle, Valid: prog.Valid}
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

	return e.graceAnswer(ctx, emit, drv, cfg, sess, Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonMaxIterations})
}

func (e *Engine) graceAnswer(ctx context.Context, emit Emit, drv Driver, cfg Config, sess *Session, stop Outcome) Outcome {
	if strings.TrimSpace(cfg.GraceInstruction) == "" || ctx.Err() != nil {
		return stop
	}
	msgs := append(append([]ai.Message(nil), sess.History...), ai.Message{Role: ai.RoleUser, Content: cfg.GraceInstruction})
	request := cfg.request(drv, msgs)
	request.ToolChoice = "none"
	out, err := e.streamGenerate(ctx, emit, request)
	if err != nil {
		return stop
	}
	sess.TokensUsed += out.Usage.TotalTokens
	sess.CostMicros += cfg.costOf(out.Usage)
	content := strings.TrimSpace(out.Message.Content)
	emit(EventAssistantDone, map[string]int{"tools": 0})
	if content == "" {
		return stop
	}
	sess.History = append(sess.History, ai.Message{Role: ai.RoleAssistant, Content: content})
	sess.History = trimHistory(sess.History, cfg.MaxHistoryMsgs)
	return Outcome{Kind: OutcomeIdle, Valid: stop.Valid, Summary: stop.Summary}
}

func clearOldToolResults(h []ai.Message, keepRecent int) []ai.Message {
	protectedFrom := len(h) - keepRecent
	out := make([]ai.Message, len(h))
	copy(out, h)
	for i := 0; i < protectedFrom; i++ {
		if out[i].Role == ai.RoleTool && len(out[i].Content) > clearableResultChars {
			out[i].Content = clearedToolResult
		}
	}
	return out
}

const reasoningChunkChars = 240

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
			if reasoning.Len() >= reasoningChunkChars || strings.Contains(ev.Token, "\n\n") {
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

func attachToolImages(sess *Session, cfg Config, images []string) {
	if len(images) == 0 {
		return
	}
	sess.History = append(sess.History, ai.Message{Role: ai.RoleUser, Content: toolImagesNote, Images: images})
	sess.History = trimHistory(sess.History, cfg.MaxHistoryMsgs)
	kept := 0
	for i := len(sess.History) - 1; i >= 0; i-- {
		m := sess.History[i]
		if m.Role != ai.RoleUser || len(m.Images) == 0 || !strings.HasPrefix(m.Content, toolImagesNote) {
			continue
		}
		if kept < cfg.KeepToolImages {
			kept++
			continue
		}
		sess.History[i] = ai.Message{Role: ai.RoleUser, Content: toolImagesCleared}
	}
}

func trimHistory(h []ai.Message, max int) []ai.Message {
	if len(h) <= max {
		return h
	}
	start := ai.HistoryWindowStart(len(h), max)
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

func truncatedResults(n int) []string {
	results := make([]string, n)
	for i := range results {
		results[i] = truncatedCallResult
	}
	return results
}
