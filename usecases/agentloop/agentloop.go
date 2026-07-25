// Package agentloop is the reusable, transport-agnostic agentic tool-use loop
// extracted from the AI Workflow Builder. It runs a bounded ReAct-style loop —
// stream a model turn, let it call tools, feed the results back, repeat — keeping
// a persistent tool-use conversation, enforcing stall/repair/iteration/token
// guards, and yielding to the user on a conversational reply.
//
// Everything domain-specific (which tools exist, how a tool call is executed, what
// "valid" and "finished" mean, how state is snapshotted to the client) lives
// behind the Driver interface. The loop itself knows nothing about workflows,
// graphs, the copilot, or any particular channel. The original Workflow-Builder
// behaviour is preserved exactly: the same reason strings, the same finish/repair
// semantics, the same empty-turn handling — so the builder rides this engine with
// no observable change, and new surfaces (the dashboard AI copilot) reuse it by
// supplying their own Driver.
//
// Layering note: this package depends only on domain/ai and domain/tools
// (downward deps). It does NOT own the transport: the caller turns its own
// connection into an Emit and runs its own session/message loop, calling Run once
// per user prompt.
package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

// Emit sends one typed event to the client. The caller wires this to its own
// transport (a WebSocket frame, an SSE event, …). The loop never touches the
// connection directly, so it stays transport-agnostic and trivially testable.
type Emit func(eventType string, payload interface{})

// Event types the engine emits. Driver-specific events (graph_snapshot, meta,
// resource_resolved, the per-tool "tool" events, idle, done) are emitted by the
// Driver / caller, not here.
const (
	EventIteration      = "iteration"
	EventAssistantDelta = "assistant_delta"
	EventReasoningDelta = "reasoning_delta"
	EventReasoningDone  = "reasoning_done"
	EventAssistantDone  = "assistant_done"
	EventTool           = "tool"
)

// Driver is the domain-specific plug for one agentic session. The engine owns the
// loop; the Driver owns the work.
type Driver interface {
	// Model is the LLM id to use for the next turn (re-read each turn so a
	// per-session model override takes effect).
	Model() string
	// SystemPrompt is the system message for the session.
	SystemPrompt() string
	// Tools are the tool definitions offered to the model this turn.
	Tools() []tools.Definition
	// Reground builds the ephemeral per-turn observation message (current state +
	// nudges). It is appended after the persistent history and is NOT stored.
	Reground(prompt string, iter, maxIter, noMutationStreak int) string
	// Dispatch executes ONE non-finish tool call, emitting any driver-specific
	// events (tool/resource/snapshot/meta) via emit. It returns the RoleTool
	// result text fed back to the model and whether state advanced this turn.
	Dispatch(ctx context.Context, call ai.ToolCall, emit Emit) StepResult
	// FinishVerdict decides a lone finish request (called only when no mutation
	// happened this turn). It owns its own summary/result text and the
	// pending-work signal that drives the repair budget.
	FinishVerdict(call ai.ToolCall) FinishResult
	// Refresh recomputes derived state WITHOUT emitting (loop start).
	Refresh()
	// AfterTurn recomputes derived state AND pushes a snapshot to the client.
	AfterTurn(emit Emit)
	// Progress is the post-turn stall/validity signal.
	Progress() Progress
}

// StepResult is the outcome of dispatching one non-finish tool call.
type StepResult struct {
	Result  string // RoleTool content fed back to the model
	Mutated bool   // did this call advance the domain state?
	// Pause, when non-nil, suspends the loop after this call — e.g. a mutation that
	// needs explicit user approval before it executes. The engine records the turn
	// so far and returns an OutcomePaused; the caller drives the out-of-band step
	// (approval) and starts a fresh Run to resume the conversation.
	Pause *Pause
}

// Pause is a request from the Driver to suspend the loop pending an out-of-band
// step (typically human approval of a mutation).
type Pause struct {
	Reason  string      // human/log summary (becomes the Outcome summary)
	Payload interface{} // arbitrary data for the caller (e.g. a pending-action id + proposed args)
}

// FinishResult is the Driver's verdict on a lone finish request.
type FinishResult struct {
	Honored      bool   // may the loop complete now?
	Summary      string // completion summary (also the honored "tool" event summary)
	Result       string // RoleTool content for the finish call
	EventSummary string // "tool" event summary when refused
	PendingWork  int    // remaining blocking work; drives the repair budget
}

// Progress is the post-turn signal used for stall detection and the validity flag.
// An empty StateHash disables the "state unchanged" guard; an empty
// BlockingSignature disables the "same problems persist" guard — so a Driver with
// no validator (e.g. the copilot) naturally relies on iteration/empty-turn limits.
type Progress struct {
	StateHash         string
	BlockingSignature string
	Valid             bool
}

// Config tunes one run. Zero fields fall back to the builder's defaults.
type Config struct {
	WorkspaceID        string
	Temperature        float32
	MaxTokensPerGen    int
	ReasoningMaxTokens int
	MaxIterations      int
	NoProgressStop     int
	RepairBudget       int
	SessionTokenBudget int // 0 = unlimited
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

// Session is the conversation state that persists across prompts within one
// connection: the agentic message history and the running token tally.
type Session struct {
	History    []ai.Message
	TokensUsed int
}

// OutcomeKind distinguishes a terminal completion from a conversational yield.
type OutcomeKind int

const (
	OutcomeDone   OutcomeKind = iota // the loop finished (success or a stop reason)
	OutcomeIdle                      // the model replied conversationally → yield to the user
	OutcomePaused                    // the Driver suspended the loop (e.g. awaiting approval)
)

// Outcome is what one Run produced. The caller emits the terminal client event
// (done/idle/proposal) so it can own that payload (residual issues, audit,
// persistence, the approval card).
type Outcome struct {
	Kind    OutcomeKind
	Valid   bool
	Summary string
	Pause   *Pause // set when Kind == OutcomePaused
}

// Engine runs the agentic loop. It is stateless apart from the AI service, so a
// single Engine value is safe to share across sessions.
type Engine struct {
	AI ai.Service
}

// Reason strings the loop yields on a stop condition. These match the AI Workflow
// Builder's original wording verbatim so its behaviour is unchanged; they are
// product-wide pt-BR copy that new surfaces reuse.
const (
	reasonMaxIterations   = "número máximo de iterações atingido"
	reasonTokenBudget     = "limite de tokens da sessão atingido"
	reasonNoProgressState = "sem progresso — o grafo não mudou nas últimas iterações"
	reasonChurn           = "sem progresso — o mesmo conjunto de problemas persiste"
	reasonRepairExhausted = "orçamento de reparo esgotado"
	reasonEmptyTurn       = "o modelo não produziu nenhuma ação — possível truncamento pelo limite de tokens de raciocínio do modelo (tente novamente ou troque de modelo)"
	finishIgnoredMsg      = "finish IGNORADO: você fez mutações neste turno — chame finish sozinho, sem outras ferramentas."
	providerErrPrefix     = "erro do provedor de IA: "
	reasonCancelled       = "cancelado"
	reasonTimeout         = "tempo limite da sessão atingido — o modelo demorou demais para responder (tente novamente ou troque para um modelo mais rápido)"
)

// sessionEndReason maps a terminated loop context to a user-facing reason. A
// deadline (the wall-clock session timeout) is distinct from a deliberate cancel.
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

// Run executes one agentic loop for a single user prompt. It streams the model's
// reasoning/answer and tool activity via emit, mutates sess (history + tokens) in
// place, and returns a terminal Outcome — the caller emits the matching done/idle
// client event. Run only returns once the loop ends (finish, idle, a stop guard,
// a provider error, or context cancellation).
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

	// Anchor the user's request at the head of the persistent conversation — the
	// standard agentic tool-use loop keeps its own message history (prior tool
	// calls + results) instead of re-planning from scratch each turn.
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

		// Messages = persistent history + a fresh ephemeral observation of the
		// current state (NOT stored — it would bloat history with full-state dumps).
		msgs := append(append([]ai.Message(nil), sess.History...),
			ai.Message{Role: ai.RoleUser, Content: drv.Reground(prompt, iter, cfg.MaxIterations, noMutationStreak)})

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

		// Normalize tool-call ids so the stored assistant message and its RoleTool
		// results share matching ids (the chat API requires this linkage on replay).
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

		// ---- empty turn (no tool calls) ----
		if len(calls) == 0 {
			// A genuine conversational reply (has text, not truncated) yields to the
			// user; a turn with no text or finish_reason=length is a truncated/empty
			// turn → retry a bounded number of times before giving up.
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

		// ---- process this turn's tool calls ----
		results := make([]string, len(calls))
		finishIdx := -1
		var finishCall ai.ToolCall
		mutated := false
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
			if step.Pause != nil {
				// The Driver suspended the loop (e.g. a mutation awaiting user
				// approval). Record the turn up to and including this call so the
				// conversation resumes coherently, then yield to the caller.
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

		// ---- finish decision (only the Driver may green-light completion) ----
		finishHonored := false
		repairExhausted := false
		finishSummary := ""
		if finishIdx >= 0 {
			switch {
			case mutated:
				// finish must be the sole intent; the model also mutated this turn.
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

		// Record the assistant turn + tool results into the persistent history BEFORE
		// any early return, so a later prompt resumes from a coherent conversation.
		recordTurn(sess, cfg.MaxHistoryMsgs, out.Message.Content, calls, results)

		if finishHonored {
			emit(EventTool, toolEvent{Name: cfg.FinishToolName, Summary: finishSummary, Ok: true})
			return Outcome{Kind: OutcomeDone, Valid: true, Summary: finishSummary}
		}
		if repairExhausted {
			return Outcome{Kind: OutcomeDone, Valid: false, Summary: reasonRepairExhausted}
		}

		// Stall guard A — the state did not change this turn (informational-call
		// dithering or repeated rejected mutations). Skipped when the Driver exposes
		// no state hash.
		if prog.StateHash != "" && prog.StateHash == prevHash {
			unchangedTurns++
			if unchangedTurns >= cfg.NoProgressStop {
				return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonNoProgressState}
			}
		} else {
			unchangedTurns = 0
		}
		prevHash = prog.StateHash

		// Stall guard B — the state keeps changing but the SAME blocking problems
		// persist (oscillation that never converges). Skipped when no signature.
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
	}

	return Outcome{Kind: OutcomeDone, Valid: prog.Valid, Summary: reasonMaxIterations}
}

// streamGenerate runs one model turn via GenerateStream, forwarding reasoning and
// answer tokens live to the client, and returns the assembled output once done.
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

// recordTurn appends the assistant turn (with its tool calls) and one RoleTool
// result per call to the persistent history, preserving the assistant→tool
// linkage the chat API requires, then bounds the history. Empty results are
// normalized to "ok" so no tool call id is left unanswered on the next request.
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

// trimHistory bounds the persistent history while keeping it valid: it retains the
// first message (the original request) and a recent suffix, and never lets the
// suffix begin with a RoleTool message (an orphan with no preceding tool call).
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
