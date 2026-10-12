package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
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
	EventAssistantRetry = "assistant_retry"
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
	ReasoningEffort    ai.ReasoningLevel
	MaxIterations      int
	IterateUntilDone   bool
	NoProgressStop     int
	RepeatedTurnStop   int
	RepairBudget       int
	SessionTokenBudget int
	EmptyTurnRetries   int
	MaxHistoryMsgs     int
	FinishToolName     string
	LogPrefix          string
	ModelLimits        ai.ModelInfo
	CompactKeepTokens  int
	CostCeilingMicros  int64
	GraceInstruction   string
	KeepToolImages     int
	ProviderRetries    int
	ProviderRetryBase  time.Duration
}

func (c Config) withDefaults() Config {
	if c.MaxIterations <= 0 && !c.IterateUntilDone {
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
	if c.CompactKeepTokens <= 0 {
		c.CompactKeepTokens = defaultCompactKeepTokens
	}
	if c.KeepToolImages <= 0 {
		c.KeepToolImages = 2
	}
	if c.ProviderRetries <= 0 {
		c.ProviderRetries = 3
	}
	if c.ProviderRetryBase <= 0 {
		c.ProviderRetryBase = time.Second
	}
	return c
}

const (
	regroundTail       = 1
	userRequestPrefix  = "PEDIDO DO USUÁRIO:\n"
	interjectionPrefix = "NOVA MENSAGEM DO USUÁRIO, enviada enquanto você trabalhava; leve-a em conta antes do próximo passo:\n"
	appNoticePrefix    = "AVISO DO APP (não é uma mensagem do usuário):\n"
)

func AppNotice(text string) string {
	return appNoticePrefix + text
}

func UserRequest(prompt string) string {
	return userRequestPrefix + prompt
}

func Interjection(prompt string) string {
	return interjectionPrefix + prompt
}

func (c Config) request(drv Driver, messages []ai.Message) ai.GenerateInput {
	return ai.GenerateInput{
		Model:              drv.Model(),
		Temperature:        c.Temperature,
		MaxTokens:          c.MaxTokensPerGen,
		ReasoningMaxTokens: c.ReasoningMaxTokens,
		ReasoningEffort:    c.ReasoningEffort,
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

func (c Config) allows(iter int) bool {
	return c.IterateUntilDone || iter <= c.MaxIterations
}


type Session struct {
	History      []ai.Message
	Turn         []ai.Message
	PromptImages []string
	Frame        func(string) string
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
	ErrRepeatedTurn    = errors.New("agentloop: the model repeated the same tool calls without converging")

	ErrProviderUnreachable = errors.New("agentloop: the connection to the AI provider kept failing after every retry")
)

type Guard interface {
	Admit(ctx context.Context) error
}

type Inbox interface {
	Drain(ctx context.Context, sess *Session) []ai.Message
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
	sameTurns := 0
	var measured contextMeasure
	overflows := 0

	frame := sess.Frame
	if frame == nil {
		frame = UserRequest
	}
	sess.History = append(sess.History, ai.Message{Role: ai.RoleUser, Content: frame(prompt), Images: sess.PromptImages})
	sess.History = trimHistory(sess.History, cfg.historyCap())

	for iter := 1; cfg.allows(iter); iter++ {
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
		if inbox, ok := drv.(Inbox); ok {
			if arrived := inbox.Drain(ctx, sess); len(arrived) > 0 {
				sess.History = trimHistory(append(sess.History, arrived...), cfg.historyCap())
			}
		}

		emit(EventIteration, iterationPayload{N: iter, Max: cfg.MaxIterations, TokensUsed: sess.TokensUsed, TokenBudget: cfg.SessionTokenBudget})
		if ceiling, compacts := cfg.compactionCeiling(); compacts && measured.estimate(drv, sess.History) >= ceiling {
			if err := e.compact(ctx, drv, cfg, sess); err != nil {
				return compactionHalt(err)
			}
			measured = contextMeasure{}
		}

		msgs := append(append([]ai.Message(nil), sess.History...),
			ai.Message{Role: ai.RoleUser, Content: drv.Reground(iter, cfg.MaxIterations, noMutationStreak)})

		out, err := e.generate(ctx, emit, cfg, cfg.request(drv, msgs))
		if err != nil {
			if ctx.Err() != nil {
				return Outcome{Kind: OutcomeDone, Valid: false, Summary: sessionEndReason(ctx.Err())}
			}
			if _, compacts := cfg.compactionCeiling(); compacts && errors.Is(err, ai.ErrContextOverflow) && overflows < maxOverflowCompactions {
				overflows++
				if err := e.compact(ctx, drv, cfg, sess); err != nil {
					return compactionHalt(err)
				}
				measured = contextMeasure{}
				continue
			}
			log.Printf("%s %s%v", cfg.LogPrefix, providerErrPrefix, err)
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: providerErrPrefix + err.Error(), Halt: fmt.Errorf("%w: %w", ErrProviderFailed, err)}
		}
		sess.TokensUsed += out.Usage.TotalTokens
		sess.CostMicros += cfg.costOf(out.Usage)
		measured = contextMeasure{tokens: out.Usage.PromptTokens + out.Usage.CompletionTokens, upTo: len(sess.History)}

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
			sess.record(cfg.historyCap(), ai.Message{Role: ai.RoleAssistant, Content: content, Reasoning: out.Message.Reasoning})
			return Outcome{Kind: OutcomeIdle, Valid: prog.Valid}
		}
		if out.FinishReason == "length" {
			truncStreak++
			recordTurn(sess, cfg.historyCap(), out.Message, calls, truncatedResults(len(calls)))
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
				recordTurn(sess, cfg.historyCap(), out.Message, calls[:i+1], results[:i+1])
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

		recordTurn(sess, cfg.historyCap(), out.Message, calls, results)
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
			sameTurns++
		} else {
			sameTurns = 1
		}
		if turnSig != "" && sameTurns >= cfg.RepeatedTurnStop {
			return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonRepeatedTurn, Halt: ErrRepeatedTurn}
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
	out, err := e.generate(ctx, emit, cfg, request)
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
	sess.record(cfg.historyCap(), ai.Message{Role: ai.RoleAssistant, Content: content, Reasoning: out.Message.Reasoning})
	return Outcome{Kind: OutcomeIdle, Valid: stop.Valid, Summary: stop.Summary}
}

const (
	reasoningChunkChars = 240
	retryGrowth         = 3
	retryJitterShare    = 5
)

type RetryNotice struct {
	Attempt   int    `json:"attempt"`
	Of        int    `json:"of"`
	Text      string `json:"text,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type streamFailure struct {
	err       error
	text      string
	reasoning string
}

func (f *streamFailure) Error() string { return f.err.Error() }
func (f *streamFailure) Unwrap() error { return f.err }

func (c Config) retryDelay(retry int) time.Duration {
	step := c.ProviderRetryBase
	for i := 0; i < retry; i++ {
		step *= retryGrowth
	}
	return step + time.Duration(rand.Int64N(int64(step)/retryJitterShare+1))
}

func (e *Engine) generate(ctx context.Context, emit Emit, cfg Config, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	for retry := 0; ; retry++ {
		out, err := e.streamGenerate(ctx, emit, input)
		if err == nil || ctx.Err() != nil || !errors.Is(err, ai.ErrProviderTransient) {
			return out, err
		}
		if retry >= cfg.ProviderRetries {
			return nil, fmt.Errorf("%w (%d attempts): %w", ErrProviderUnreachable, retry+1, err)
		}
		delay := cfg.retryDelay(retry)
		log.Printf("%s %s%v; retrying the same step in %s (%d of %d)", cfg.LogPrefix, providerErrPrefix, err, delay.Round(time.Millisecond), retry+1, cfg.ProviderRetries)
		notice := RetryNotice{Attempt: retry + 1, Of: cfg.ProviderRetries}
		var shown *streamFailure
		if errors.As(err, &shown) {
			notice.Text, notice.Reasoning = shown.text, shown.reasoning
		}
		emit(EventAssistantRetry, notice)
		if !waitFor(ctx, delay) {
			return nil, ctx.Err()
		}
	}
}

func waitFor(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func (e *Engine) streamGenerate(ctx context.Context, emit Emit, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	ch, err := e.AI.GenerateStream(ctx, input)
	if err != nil {
		return nil, err
	}
	out := &ai.GenerateOutput{}
	var content, pending, reasoning, shownReasoning strings.Builder
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
			shownReasoning.WriteString(ev.Token)
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
			out.Message.Reasoning = ev.Reasoning
			if ev.Usage != nil {
				out.Usage = *ev.Usage
			}
			if content.Len() == 0 {
				content.WriteString(ev.FullText)
			}
		case ai.StreamEventError:
			flush()
			endReasoning()
			return nil, &streamFailure{err: ev.Error, text: content.String(), reasoning: shownReasoning.String()}
		}
	}
	flush()
	endReasoning()
	out.Message.Role, out.Message.Content = ai.RoleAssistant, content.String()
	return out, nil
}

func recordTurn(sess *Session, maxHistory int, said ai.Message, calls []ai.ToolCall, results []string) {
	step := []ai.Message{{Role: ai.RoleAssistant, Content: said.Content, ToolCalls: calls, Reasoning: said.Reasoning}}
	for i, tc := range calls {
		r := ""
		if i < len(results) {
			r = results[i]
		}
		if strings.TrimSpace(r) == "" {
			r = "ok"
		}
		step = append(step, ai.Message{Role: ai.RoleTool, ToolCallID: tc.ID, Content: r})
	}
	sess.record(maxHistory, step...)
}

func (s *Session) TakeTurn() []ai.Message {
	turn := s.Turn
	s.Turn = nil
	return turn
}

func (s *Session) record(maxHistory int, msgs ...ai.Message) {
	s.History = trimHistory(append(s.History, msgs...), maxHistory)
	s.Turn = append(s.Turn, msgs...)
}

func attachToolImages(sess *Session, cfg Config, images []string) {
	if len(images) == 0 {
		return
	}
	sess.History = append(sess.History, ai.Message{Role: ai.RoleUser, Content: toolImagesNote, Images: images})
	sess.History = trimHistory(sess.History, cfg.historyCap())
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
	if max <= 0 || len(h) <= max {
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
