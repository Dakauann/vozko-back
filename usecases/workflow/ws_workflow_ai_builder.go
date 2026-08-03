package workflow_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vozko/brand"
	"vozko/domain/ai"
	"vozko/domain/tools"
	"vozko/domain/workflow"
	"vozko/usecases/agentloop"
	"vozko/usecases/workflow/node_executors"
)

// AIBuilderUseCase is the real-time AI Workflow Builder copilot. From
// natural-language prompts it CREATES workflows from scratch and EDITS existing
// ones across all registered node types, running a bounded agentic loop that can
// only finish when the domain validator (LintReport.IsGreen) passes.
//
// It is BUILD-ONLY: it mutates an in-memory graph and streams snapshots, but
// never persists. The frontend saves the streamed graph through the existing
// create/update workflow HTTP path (which keeps permission + workspace gating).
//
// Layer note: this usecase calls the pure domain LintGraph and the node_executors
// dynamic-handle helpers (downward deps only). It mirrors the discipline of the
// workflow-simulation vertical (HandleSession shape, writeJSON mutex+deadline,
// reader goroutine + cancellation), without reusing its private members.

// ---- tool names ----------------------------------------------------------

const (
	toolGetNodeSpec  = "get_node_spec"
	toolFindResource = "find_resource"
	toolAddNode      = "add_node"
	toolConnect      = "connect"
	toolUpdateNode   = "update_node"
	toolRemoveNode   = "remove_node"
	toolRemoveEdge   = "remove_edge"
	toolSetMeta      = "set_meta"
	toolFinish       = "finish"
)

// ---- loop bounds ---------------------------------------------------------

const (
	builderMaxIterations  = 30
	builderNoProgressStop = 5
	builderRepairBudget   = 3
	// builderMaxTokensPerGen is the per-turn output budget. It must be generous:
	// reasoning models (Gemini 3, etc.) count thinking tokens against this budget,
	// so a small cap (the old 4096) let thinking consume everything and the model
	// emitted no tool call, a silent "empty turn". Paired with a reasoning cap
	// below so output always has room.
	builderMaxTokensPerGen = 24000
	// builderReasoningMaxTokens caps chain-of-thought per turn so it can't eat the
	// whole output budget (OpenRouter `reasoning.max_tokens`).
	builderReasoningMaxTokens = 10000
	// builderEmptyTurnRetries is how many consecutive empty/truncated turns (no
	// tool call AND no usable text) we retry before giving up with a clear error,
	// instead of mistaking a truncated turn for a finished conversation.
	builderEmptyTurnRetries = 2
	// builderMaxHistoryMsgs bounds the persistent agentic message history so a long
	// session can't grow the context without limit (assistant↔tool linkage preserved).
	builderMaxHistoryMsgs = 80
	builderLogTail        = 24 // keep the most recent N action-log lines (internal audit)
)

// builderSessionSeq gives each builder session a monotonic id for log correlation.
var builderSessionSeq int64

var resourceKinds = []string{
	"ai_models", "agents", "templates", "departments", "medias",
	"labels", "members", "mcp_collections", "knowledge_bases", "business_phones", "workflows",
}

// ---- collaborators -------------------------------------------------------

// ResourceMatch is one workspace-scoped resource the AI can reference by id.
type ResourceMatch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ResourceResolver resolves a human resource name → workspace-scoped ids. It MUST
// be workspace-scoped per kind: a query in workspace A must never return a
// workspace-B resource.
type ResourceResolver interface {
	Search(ctx context.Context, workspaceID, kind, query string, limit int) ([]ResourceMatch, error)
}

// BuilderAuditRecord is a minimal compliance record of one build session, the
// builder produces savable/activatable automations (WhatsApp/SMS/email,
// department transfers), so this is a requirement, not optional.
type BuilderAuditRecord struct {
	WorkspaceID    string
	WorkflowID     string
	Mode           string // "create" | "edit"
	Prompts        []string
	Model          string
	FinalGraphHash string
	Valid          bool
	TokensUsed     int
	At             time.Time
}

// BuilderAuditSink persists builder-session audit records.
type BuilderAuditSink interface {
	Record(ctx context.Context, rec BuilderAuditRecord) error
}

// BalanceGate reports whether a workspace can afford to keep spending. The builder
// uses it to refuse a build turn before any model call when the balance is
// exhausted, post-hoc metering can't prevent unmetered spend on its own.
type BalanceGate interface {
	HasSufficientBalance(workspaceID string, amountMicros int64) (bool, error)
}

type AIBuilderUseCaseDeps struct {
	WorkflowRepo              workflow.WorkflowRepository
	AIService                 ai.Service
	NodeCatalogFn             func() []workflow.NodeDefinition
	ResourceResolver          ResourceResolver // optional
	ModelLookup               ModelLookup      // optional; cached model-id validity check
	AuditSink                 BuilderAuditSink // optional
	Model                     string
	MaxTokens                 int           // per-session token budget (0 = unlimited)
	SessionTimeout            time.Duration // wall-clock per build loop (0 = no deadline)
	MaxConcurrentPerWorkspace int           // 0 = unlimited

	// Balance gating (optional). When BalanceGate is set, each build turn is
	// refused up-front unless the workspace holds at least MinBalanceMicros
	// (a strictly positive balance by default). Post-hoc metering alone cannot
	// stop a zero/negative-balance workspace from accruing AI cost, this can.
	BalanceGate      BalanceGate
	MinBalanceMicros int64

	// Loop bounds (0 => sensible defaults). Exposed for testing and tuning.
	MaxIterations  int
	NoProgressStop int
	RepairBudget   int
}

type AIBuilderUseCase interface {
	HandleSession(ctx context.Context, conn BuilderConn, workflowID, workspaceID string) error
}

func NewAIBuilderUseCase(deps AIBuilderUseCaseDeps) AIBuilderUseCase {
	if deps.Model == "" {
		deps.Model = "anthropic/claude-sonnet-4"
	}
	uc := &aiBuilderUC{
		deps:       deps,
		engine:     agentloop.Engine{AI: deps.AIService},
		active:     make(map[string]int),
		maxIter:    deps.MaxIterations,
		noProgress: deps.NoProgressStop,
		repairCap:  deps.RepairBudget,
	}
	if uc.maxIter <= 0 {
		uc.maxIter = builderMaxIterations
	}
	if uc.noProgress <= 0 {
		uc.noProgress = builderNoProgressStop
	}
	if uc.repairCap <= 0 {
		uc.repairCap = builderRepairBudget
	}
	return uc
}

type aiBuilderUC struct {
	deps       AIBuilderUseCaseDeps
	engine     agentloop.Engine
	mu         sync.Mutex
	active     map[string]int
	maxIter    int
	noProgress int
	repairCap  int
}

// ---- WS protocol ---------------------------------------------------------

type aiBuilderServerMsg struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type aiBuilderClientMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type builderReadyPayload struct {
	WorkflowType string `json:"workflowType"`
	NodeCount    int    `json:"nodeCount"`
	Mode         string `json:"mode"`
	Model        string `json:"model"`
}

// toolEventPayload narrates one tool call the agent made (with its result or
// rejection) so the UI can show exactly what the agent is doing.
type toolEventPayload struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Ok      bool   `json:"ok"`
}

type setModelData struct {
	Model string `json:"model"`
}

// metaPayload propagates workflow-level metadata (name/description/type) the AI
// sets via set_meta, so the editor's form fields stay in sync.
type metaPayload struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	WorkflowType string `json:"workflowType"`
}

type graphSnapshotPayload struct {
	Graph  workflow.Graph       `json:"graph"`
	Issues []workflow.LintIssue `json:"issues"`
	Valid  bool                 `json:"valid"`
}

type donePayload struct {
	Valid          bool                 `json:"valid"`
	Summary        string               `json:"summary"`
	ResidualIssues []workflow.LintIssue `json:"residualIssues,omitempty"`
}

type resourceResolvedPayload struct {
	Kind    string          `json:"kind"`
	Query   string          `json:"query"`
	Matches []ResourceMatch `json:"matches"`
}

type promptData struct {
	Text string `json:"text"`
	// Graph is the client's live canvas at the instant the prompt was sent. When
	// present the server adopts it before running the turn, so the agent always
	// sees the user's manual edits since connect (moves/config/added/removed
	// nodes), never a stale server-side snapshot. Optional (nil => keep current).
	Graph *workflow.Graph `json:"graph,omitempty"`
}
type setTypeData struct {
	Type string `json:"type"`
}

// hydrateData carries the client's current canvas graph so a reconnected session
// adopts what the user is actually looking at, instead of its empty (create) or
// last-saved (edit) state. Sent by the client on (re)connect; see applyHydrate.
type hydrateData struct {
	Graph *workflow.Graph `json:"graph"`
}

// ---- builder state -------------------------------------------------------

type builderState struct {
	workspaceID string
	workflowID  string // "" => create-from-scratch
	mode        string // "create" | "edit"
	wfType      workflow.WorkflowType
	name        string
	description string

	model       string // per-session LLM model (overridable via set_model)
	graph       *workflow.Graph
	fullCatalog []workflow.NodeDefinition // unfiltered registry catalog

	exempt     map[string]bool
	lastReport workflow.LintReport

	actionLog []string // "what I did" (bounded tail), internal audit, not sent to the model
	prompts   []string // all user prompts this session (for audit)

	nextID         int
	clientIDs      map[string]string // add_node client_id -> server id (idempotency)
	sessionID      int64             // for log correlation
	inspectedSpecs map[string]bool   // get_node_spec calls already served (anti-dither)
	searchCache    map[string]string // find_resource (kind:query) -> result (anti-dither)
}

// ---- session lifecycle ---------------------------------------------------

func (uc *aiBuilderUC) HandleSession(ctx context.Context, conn BuilderConn, workflowID, workspaceID string) error {
	var writeMu sync.Mutex
	emit := func(eventType string, payload interface{}) {
		_ = uc.writeJSON(conn, &writeMu, aiBuilderServerMsg{Type: eventType, Payload: payload})
	}

	if !uc.acquire(workspaceID) {
		return uc.sendError(conn, &writeMu, "muitas sessões de construção simultâneas neste workspace, tente novamente em instantes")
	}
	defer uc.release(workspaceID)

	st, err := uc.initState(workflowID, workspaceID)
	if err != nil {
		return uc.sendError(conn, &writeMu, err.Error())
	}

	uc.relint(st)
	emit("builder_ready", builderReadyPayload{
		WorkflowType: string(st.wfType), NodeCount: len(st.graph.Nodes), Mode: st.mode, Model: st.model,
	})
	uc.snapshot(emit, st)

	drv := &builderDriver{uc: uc, st: st}
	sess := &agentloop.Session{}

	promptCh := make(chan promptData, 4)
	typeCh := make(chan string, 2)
	modelCh := make(chan string, 2)
	hydrateCh := make(chan *workflow.Graph, 2)
	closeCh := make(chan struct{})

	var loopCancelMu sync.Mutex
	var loopCancel context.CancelFunc
	cancelInFlight := func() {
		loopCancelMu.Lock()
		if loopCancel != nil {
			loopCancel()
		}
		loopCancelMu.Unlock()
	}

	go func() {
		// On disconnect, cancel any in-flight build loop so the agent stops
		// working (and stops spending) the moment the client goes away.
		defer func() {
			cancelInFlight()
			close(closeCh)
		}()
		for {
			_, raw, rerr := conn.ReadMessage()
			if rerr != nil {
				return
			}
			var msg aiBuilderClientMsg
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "user_prompt":
				var d promptData
				if json.Unmarshal(msg.Data, &d) == nil && strings.TrimSpace(d.Text) != "" {
					select {
					case promptCh <- d:
					default:
					}
				}
			case "set_workflow_type":
				var d setTypeData
				if json.Unmarshal(msg.Data, &d) == nil {
					select {
					case typeCh <- d.Type:
					default:
					}
				}
			case "set_model":
				var d setModelData
				if json.Unmarshal(msg.Data, &d) == nil && strings.TrimSpace(d.Model) != "" {
					select {
					case modelCh <- d.Model:
					default:
					}
				}
			case "hydrate_graph":
				var d hydrateData
				if json.Unmarshal(msg.Data, &d) == nil && d.Graph != nil {
					select {
					case hydrateCh <- d.Graph:
					default:
					}
				}
			case "cancel":
				cancelInFlight()
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-closeCh:
			return nil
		case t := <-typeCh:
			uc.applyWorkflowType(st, t)
			uc.relint(st)
			uc.snapshot(emit, st)
		case m := <-modelCh:
			st.model = m
		case g := <-hydrateCh:
			// The client re-sends its canvas on (re)connect. Adopt it as the working
			// graph so a session that lost its in-memory state doesn't rebuild from
			// scratch (which would clobber the canvas on the next edit). Only handled
			// while idle here, a running build blocks this loop, so there is never a
			// concurrent writer of st.graph.
			uc.applyHydrate(st, g)
			uc.relint(st)
			uc.snapshot(emit, st)
		case turn := <-promptCh:
			// Adopt the canvas the user is looking at RIGHT NOW before building, so
			// the agent works from their manual edits since connect (moves/config/
			// added/removed nodes) instead of the last server-side snapshot. Done
			// here in the main loop, never concurrently with a build, so st.graph
			// keeps a single writer. Carried on the prompt (not a separate message)
			// so it can't be reordered after the build starts.
			if turn.Graph != nil {
				uc.applyHydrate(st, turn.Graph)
				uc.relint(st)
			}
			// Gate BEFORE any model call: a workspace with no balance must not be
			// able to start a build turn (and accrue cost). Stays connected so the
			// user can top up and send again.
			if !uc.guardBalance(ctx, emit, st) {
				continue
			}
			st.prompts = append(st.prompts, turn.Text)
			lctx := ctx
			var cancel context.CancelFunc
			if uc.deps.SessionTimeout > 0 {
				lctx, cancel = context.WithTimeout(ctx, uc.deps.SessionTimeout)
			} else {
				lctx, cancel = context.WithCancel(ctx)
			}
			loopCancelMu.Lock()
			loopCancel = cancel
			loopCancelMu.Unlock()

			outcome := uc.engine.Run(lctx, emit, drv, uc.builderConfig(st), sess, turn.Text)
			switch outcome.Kind {
			case agentloop.OutcomeIdle:
				log.Printf("[wf-ai-builder] s%d IDLE (resposta conversacional, sem ações), yielding to user, valid=%v", st.sessionID, outcome.Valid)
				emit("idle", map[string]bool{"valid": outcome.Valid})
			default:
				uc.emitDone(ctx, emit, st, outcome.Valid, outcome.Summary, sess.TokensUsed)
			}

			loopCancelMu.Lock()
			loopCancel = nil
			loopCancelMu.Unlock()
			cancel()
		}
	}
}

func (uc *aiBuilderUC) initState(workflowID, workspaceID string) (*builderState, error) {
	var catalog []workflow.NodeDefinition
	if uc.deps.NodeCatalogFn != nil {
		catalog = uc.deps.NodeCatalogFn()
	}
	st := &builderState{
		workspaceID:    workspaceID,
		workflowID:     workflowID,
		model:          uc.deps.Model,
		fullCatalog:    catalog,
		graph:          &workflow.Graph{},
		clientIDs:      make(map[string]string),
		inspectedSpecs: make(map[string]bool),
		searchCache:    make(map[string]string),
		sessionID:      atomic.AddInt64(&builderSessionSeq, 1),
		wfType:         workflow.WorkflowTypeMessages,
	}
	if workflowID == "" {
		st.mode = "create"
		return st, nil
	}

	st.mode = "edit"
	wf, err := uc.deps.WorkflowRepo.FindByID(workflowID)
	if err != nil {
		return nil, fmt.Errorf("falha ao carregar workflow: %v", err)
	}
	if wf == nil {
		return nil, fmt.Errorf("workflow não encontrado")
	}
	// Defense-in-depth: re-assert workspace ownership (the CRUD path lacks a filter).
	if wf.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("workflow pertence a outro workspace")
	}
	wf.Normalize()
	g := wf.Graph
	st.graph = &g
	st.wfType = wf.Type
	st.name = wf.Name
	st.description = wf.Description
	return st, nil
}

func (uc *aiBuilderUC) applyWorkflowType(st *builderState, t string) {
	if workflow.WorkflowType(t) == workflow.WorkflowTypeMessages {
		st.wfType = workflow.WorkflowTypeMessages
	}
}

// applyHydrate replaces the in-memory graph with the client's current canvas
// (re-sent on reconnect). The freshly opened session would otherwise start from
// an empty (create) or last-saved (edit) graph, so the next edit would build on
// stale state and wipe the canvas. Node ids from the canvas are preserved;
// per-session idempotency bookkeeping is reset so it never maps stale client ids.
func (uc *aiBuilderUC) applyHydrate(st *builderState, g *workflow.Graph) {
	if g == nil {
		return
	}
	st.graph = g
	st.clientIDs = make(map[string]string)
}

// ---- concurrency cap -----------------------------------------------------

func (uc *aiBuilderUC) acquire(workspaceID string) bool {
	if uc.deps.MaxConcurrentPerWorkspace <= 0 {
		return true
	}
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.active[workspaceID] >= uc.deps.MaxConcurrentPerWorkspace {
		return false
	}
	uc.active[workspaceID]++
	return true
}

func (uc *aiBuilderUC) release(workspaceID string) {
	if uc.deps.MaxConcurrentPerWorkspace <= 0 {
		return
	}
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.active[workspaceID] > 0 {
		uc.active[workspaceID]--
	}
}

// guardBalance refuses a build turn when the workspace can't cover it. Fail-closed:
// a balance-lookup error blocks the turn rather than letting unmetered AI spend
// through. No gate is applied when BalanceGate is unset. Returns true to proceed.
func (uc *aiBuilderUC) guardBalance(ctx context.Context, emit agentloop.Emit, st *builderState) bool {
	if uc.deps.BalanceGate == nil {
		return true
	}
	minMicros := uc.deps.MinBalanceMicros
	if minMicros < 1 {
		minMicros = 1 // default: require a strictly positive balance
	}
	ok, err := uc.deps.BalanceGate.HasSufficientBalance(st.workspaceID, minMicros)
	if err != nil {
		log.Printf("[wf-ai-builder] s%d balance check failed for workspace %s (fail-closed): %v", st.sessionID, st.workspaceID, err)
		uc.emitDone(ctx, emit, st, false, "não foi possível verificar o saldo do workspace, tente novamente em instantes", 0)
		return false
	}
	if !ok {
		log.Printf("[wf-ai-builder] s%d refused: insufficient balance for workspace %s", st.sessionID, st.workspaceID)
		uc.emitDone(ctx, emit, st, false, "saldo insuficiente para usar o construtor de IA, adicione créditos para continuar", 0)
		return false
	}
	return true
}

func (uc *aiBuilderUC) relint(st *builderState) {
	st.exempt = collectExecuteModeLeafNodes(st.graph)
	st.lastReport = workflow.LintGraph(st.graph, st.wfType, st.fullCatalog, st.exempt, builderHandleResolver)
}

func builderHandleResolver(n workflow.Node) ([]workflow.HandleDefinition, bool) {
	switch n.Type {
	case workflow.NodeTypeConditionTextMatch:
		return node_executors.TextMatchOutputs(n.Config), true
	case workflow.NodeTypeActionAIAgent:
		return node_executors.AIAgentToolOutputs(n.Config), true
	case workflow.NodeTypeActionSendInteractive:
		return node_executors.AskInteractiveOutputs(n.Config), true
	}
	return nil, false
}

// ResolveNodeHandles is the single backend authority for a node's output handles
// and their optional flags: config-dependent (dynamic) handles when applicable,
// ai_agent tool routes + response, text_match cases, otherwise the node type's
// static catalog handles. The builder lint, activation, and the resolve-handles
// API all go through this, so every surface (including the frontend) agrees.
func ResolveNodeHandles(catalog []workflow.NodeDefinition, n workflow.Node) []workflow.HandleDefinition {
	if h, ok := builderHandleResolver(n); ok {
		return h
	}
	if def, ok := workflow.NodeCatalogMap(catalog)[n.Type]; ok {
		return def.Outputs
	}
	return nil
}

// LintWorkflowGraph runs the full builder/activation lint over a graph and returns
// the structured issues, the SAME rules the activation gate enforces, via the
// SAME dynamic-handle resolver, so the frontend can surface and highlight
// problems (per node/handle) before activating, without re-deriving any rule.
func LintWorkflowGraph(catalog []workflow.NodeDefinition, w *workflow.Workflow) workflow.LintReport {
	w.Normalize()
	exempt := collectExecuteModeLeafNodes(&w.Graph)
	return workflow.LintGraph(&w.Graph, w.Type, catalog, exempt, builderHandleResolver)
}

func (uc *aiBuilderUC) snapshot(emit agentloop.Emit, st *builderState) {
	emit("graph_snapshot", graphSnapshotPayload{
		Graph:  *st.graph,
		Issues: st.lastReport.Issues,
		Valid:  st.lastReport.IsGreen(),
	})
}

func (uc *aiBuilderUC) emitDone(ctx context.Context, emit agentloop.Emit, st *builderState, valid bool, summary string, tokensUsed int) {
	log.Printf("[wf-ai-builder] s%d DONE valid=%v blocking=%d tokens=%d reason=%q",
		st.sessionID, valid, len(st.lastReport.Blocking()), tokensUsed, summary)
	// Persist the audit trail BEFORE notifying the client it's done, the record
	// is the compliance source of truth for what the builder produced.
	if uc.deps.AuditSink != nil {
		// Use a detached context so a cancelled/timed-out build still records.
		_ = uc.deps.AuditSink.Record(context.WithoutCancel(ctx), BuilderAuditRecord{
			WorkspaceID:    st.workspaceID,
			WorkflowID:     st.workflowID,
			Mode:           st.mode,
			Prompts:        append([]string(nil), st.prompts...),
			Model:          uc.deps.Model,
			FinalGraphHash: graphHash(st.graph),
			Valid:          valid,
			TokensUsed:     tokensUsed,
			At:             time.Now().UTC(),
		})
	}
	emit("done", donePayload{Valid: valid, Summary: summary, ResidualIssues: st.lastReport.Blocking()})
}

// ---- agentloop driver ----------------------------------------------------

// builderConfig returns the agentloop tuning for one Workflow-Builder session.
func (uc *aiBuilderUC) builderConfig(st *builderState) agentloop.Config {
	return agentloop.Config{
		WorkspaceID:        st.workspaceID,
		Temperature:        0,
		MaxTokensPerGen:    builderMaxTokensPerGen,
		ReasoningMaxTokens: builderReasoningMaxTokens,
		MaxIterations:      uc.maxIter,
		NoProgressStop:     uc.noProgress,
		RepairBudget:       uc.repairCap,
		SessionTokenBudget: uc.deps.MaxTokens,
		EmptyTurnRetries:   builderEmptyTurnRetries,
		MaxHistoryMsgs:     builderMaxHistoryMsgs,
		FinishToolName:     toolFinish,
		LogPrefix:          fmt.Sprintf("[wf-ai-builder] s%d", st.sessionID),
	}
}

// builderDriver adapts one builder session's state to the generic agentloop.Driver
// seam: it offers the workflow tools, dispatches each tool call onto the in-memory
// graph (emitting tool/snapshot/meta/resource events), and only green-lights
// finish when the domain validator passes.
type builderDriver struct {
	uc *aiBuilderUC
	st *builderState
}

func (d *builderDriver) Model() string             { return d.st.model }
func (d *builderDriver) SystemPrompt() string      { return d.uc.systemPrompt(d.st) }
func (d *builderDriver) Tools() []tools.Definition { return d.uc.builderTools(d.st) }

func (d *builderDriver) Reground(iter, maxIter, noMutationStreak int) string {
	return d.uc.regroundMessage(d.st, iter, maxIter, noMutationStreak)
}

func (d *builderDriver) Refresh() { d.uc.relint(d.st) }

func (d *builderDriver) AfterTurn(emit agentloop.Emit) {
	d.uc.relint(d.st)
	d.uc.snapshot(emit, d.st)
}

func (d *builderDriver) Progress() agentloop.Progress {
	return agentloop.Progress{
		StateHash:         graphHash(d.st.graph),
		BlockingSignature: blockingSignature(d.st.lastReport),
		Valid:             d.st.lastReport.IsGreen(),
	}
}

func (d *builderDriver) Dispatch(ctx context.Context, tc ai.ToolCall, emit agentloop.Emit) agentloop.StepResult {
	switch tc.Name {
	case toolGetNodeSpec:
		nt, _ := tc.Arguments["node_type"].(string)
		res := d.uc.handleGetNodeSpec(d.st, tc)
		emit("tool", toolEventPayload{Name: toolGetNodeSpec, Summary: "consultou a especificação de " + nt, Ok: true})
		return agentloop.StepResult{Result: res}
	case toolFindResource:
		kind, _ := tc.Arguments["kind"].(string)
		query, _ := tc.Arguments["query"].(string)
		res := d.uc.handleFindResource(ctx, emit, d.st, tc)
		emit("tool", toolEventPayload{Name: toolFindResource, Summary: fmt.Sprintf("buscou %s \"%s\"", kind, query), Ok: true})
		return agentloop.StepResult{Result: res}
	default:
		sig := mutationSignature(tc)
		delta, err := d.uc.applyMutation(d.st, tc)
		if err != nil {
			r := "REJEITADO " + tc.Name + ": " + err.Error()
			d.st.pushLog(r)
			emit("tool", toolEventPayload{Name: tc.Name, Summary: err.Error(), Ok: false})
			return agentloop.StepResult{Result: r, Signature: sig}
		}
		d.st.pushLog(delta)
		emit("tool", toolEventPayload{Name: tc.Name, Summary: delta, Ok: true})
		if tc.Name == toolSetMeta {
			emit("meta", metaPayload{Name: d.st.name, Description: d.st.description, WorkflowType: string(d.st.wfType)})
		}
		return agentloop.StepResult{Result: delta, Mutated: true, Signature: sig}
	}
}

func (d *builderDriver) FinishVerdict(call ai.ToolCall) agentloop.FinishResult {
	summary, _ := call.Arguments["summary"].(string)
	if d.st.lastReport.IsGreen() {
		if summary == "" {
			summary = "workflow validado"
		}
		return agentloop.FinishResult{Honored: true, Summary: summary, Result: "finish ACEITO: " + summary}
	}
	cur := len(d.st.lastReport.Blocking())
	r := fmt.Sprintf("finish RECUSADO: ainda há %d problema(s) bloqueante(s), corrija-os antes de finalizar.", cur)
	d.st.pushLog(r)
	return agentloop.FinishResult{
		Honored:      false,
		Result:       r,
		EventSummary: fmt.Sprintf("recusado: %d problema(s) bloqueante(s) restante(s)", cur),
		PendingWork:  cur,
	}
}

// ---- mutation guard ------------------------------------------------------

// validateConfigResourceIDs rejects config values that reference a resource by id
// but don't match a real one. It is schema-driven, it walks the node's
// ConfigSchema and checks every field whose OptionsSource names a resource kind,
// so new resource-backed fields are covered automatically. Only kinds the builder
// can actually verify are checked (see resourceIDValid); the rest are skipped so
// we never raise a false rejection.
func (uc *aiBuilderUC) validateConfigResourceIDs(st *builderState, nt workflow.NodeType, cfg map[string]interface{}) error {
	if len(cfg) == 0 {
		return nil
	}
	def, ok := workflow.NodeCatalogMap(st.fullCatalog)[nt]
	if !ok {
		return nil
	}
	for _, f := range def.ConfigSchema {
		if f.OptionsSource == "" {
			continue
		}
		raw, present := cfg[f.Key]
		if !present {
			continue
		}
		val, _ := raw.(string)
		val = strings.TrimSpace(val)
		if val == "" || strings.Contains(val, "{{") { // unset or templated → nothing to check
			continue
		}
		valid, checkable := uc.resourceIDValid(f.OptionsSource, val)
		if checkable && !valid {
			return fmt.Errorf("valor %q inválido para o campo %q: não corresponde a nenhum %s real. Use find_resource(%s, ...) para listar ids válidos e copie um EXATAMENTE como retornado, NUNCA invente nomes ou sufixos (ex.: '-latest').",
				val, f.Key, f.OptionsSource, f.OptionsSource)
		}
	}
	return nil
}

// resourceIDValid reports whether id is a real id for kind via a targeted, cached
// membership check (it validates only the id in question, it never loads the
// whole catalog into the builder). checkable=false means the builder cannot
// verify this kind here (workspace-scoped opaque ids the model already resolved
// via find_resource, or a lookup that's temporarily unavailable) and the caller
// must not reject. Extend the switch to make more kinds verifiable.
func (uc *aiBuilderUC) resourceIDValid(kind, id string) (valid, checkable bool) {
	switch kind {
	case "ai_models":
		if uc.deps.ModelLookup == nil {
			return false, false
		}
		ok, err := uc.deps.ModelLookup.IsValidModel(context.Background(), id)
		if err != nil {
			return false, false // catalog unavailable → don't block
		}
		return ok, true
	default:
		return false, false
	}
}

func (uc *aiBuilderUC) applyMutation(st *builderState, tc ai.ToolCall) (string, error) {
	switch tc.Name {
	case toolSetMeta:
		return uc.applySetMeta(st, tc)
	case toolAddNode:
		return uc.applyAddNode(st, tc)
	case toolConnect:
		return uc.applyConnect(st, tc)
	case toolUpdateNode:
		return uc.applyUpdateNode(st, tc)
	case toolRemoveNode:
		return uc.applyRemoveNode(st, tc)
	case toolRemoveEdge:
		return uc.applyRemoveEdge(st, tc)
	default:
		return "", fmt.Errorf("ferramenta desconhecida %q", tc.Name)
	}
}

func (uc *aiBuilderUC) applySetMeta(st *builderState, tc ai.ToolCall) (string, error) {
	var parts []string
	if v, ok := tc.Arguments["name"].(string); ok && strings.TrimSpace(v) != "" {
		st.name = v
		parts = append(parts, "nome="+v)
	}
	if v, ok := tc.Arguments["description"].(string); ok && strings.TrimSpace(v) != "" {
		st.description = v
		parts = append(parts, "descrição definida")
	}
	if v, ok := tc.Arguments["workflow_type"].(string); ok && strings.TrimSpace(v) != "" {
		if v != string(workflow.WorkflowTypeMessages) {
			return "", fmt.Errorf("workflow_type inválido %q (use 'messages')", v)
		}
		uc.applyWorkflowType(st, v)
		parts = append(parts, "tipo="+v)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("set_meta sem campos válidos")
	}
	return "set_meta: " + strings.Join(parts, ", "), nil
}

func (uc *aiBuilderUC) applyAddNode(st *builderState, tc ai.ToolCall) (string, error) {
	typeStr, _ := tc.Arguments["type"].(string)
	nt := workflow.NodeType(strings.TrimSpace(typeStr))
	if err := uc.validateNodeType(st, nt); err != nil {
		return "", err
	}

	// Idempotency on client_id.
	if cid, ok := tc.Arguments["client_id"].(string); ok && cid != "" {
		if existing, seen := st.clientIDs[cid]; seen {
			return fmt.Sprintf("add_node ignorado (client_id %q já criou o nó %q)", cid, existing), nil
		}
	}

	cfg, err := parseConfigJSON(tc.Arguments["config_json"])
	if err != nil {
		return "", err
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if err := uc.validateConfigResourceIDs(st, nt, cfg); err != nil {
		return "", err
	}

	id := uc.freshNodeID(st)
	node := workflow.Node{
		ID:       id,
		Type:     nt,
		Position: workflow.Position{X: float64(100 + (len(st.graph.Nodes)%6)*220), Y: float64(100 + (len(st.graph.Nodes)/6)*140)},
		Config:   cfg,
	}
	st.graph.Nodes = append(st.graph.Nodes, node)
	if cid, ok := tc.Arguments["client_id"].(string); ok && cid != "" {
		st.clientIDs[cid] = id
	}
	label := ""
	if l, ok := tc.Arguments["label"].(string); ok {
		label = " (" + l + ")"
	}
	return fmt.Sprintf("add_node %s%s → id=%s", nt, label, id), nil
}

func (uc *aiBuilderUC) applyConnect(st *builderState, tc ai.ToolCall) (string, error) {
	source, _ := tc.Arguments["source"].(string)
	target, _ := tc.Arguments["target"].(string)
	label, _ := tc.Arguments["label"].(string)
	if st.graph.FindNode(source) == nil {
		return "", fmt.Errorf("connect: nó de origem %q não existe", source)
	}
	if st.graph.FindNode(target) == nil {
		return "", fmt.Errorf("connect: nó de destino %q não existe", target)
	}
	for _, e := range st.graph.Edges {
		if e.Source == source && e.Target == target && e.Label == label {
			return fmt.Sprintf("connect %s→%s (rótulo %q) já existe", source, target, label), nil
		}
	}
	st.graph.Edges = append(st.graph.Edges, workflow.Edge{Source: source, Target: target, Label: label})
	if label != "" {
		return fmt.Sprintf("connect %s→%s (rótulo %q)", source, target, label), nil
	}
	return fmt.Sprintf("connect %s→%s", source, target), nil
}

func (uc *aiBuilderUC) applyUpdateNode(st *builderState, tc ai.ToolCall) (string, error) {
	id, _ := tc.Arguments["id"].(string)
	node := st.graph.FindNode(id)
	if node == nil {
		return "", fmt.Errorf("update_node: nó %q não existe", id)
	}
	cfg, err := parseConfigJSON(tc.Arguments["config_json"])
	if err != nil {
		return "", err
	}
	if err := uc.validateConfigResourceIDs(st, node.Type, cfg); err != nil {
		return "", err
	}
	if cfg != nil {
		if node.Config == nil {
			node.Config = map[string]interface{}{}
		}
		for k, v := range cfg {
			node.Config[k] = v
		}
	}
	return fmt.Sprintf("update_node %s", id), nil
}

func (uc *aiBuilderUC) applyRemoveNode(st *builderState, tc ai.ToolCall) (string, error) {
	id, _ := tc.Arguments["id"].(string)
	if st.graph.FindNode(id) == nil {
		return fmt.Sprintf("remove_node %s (já ausente)", id), nil
	}
	nodes := st.graph.Nodes[:0]
	for _, n := range st.graph.Nodes {
		if n.ID != id {
			nodes = append(nodes, n)
		}
	}
	st.graph.Nodes = nodes
	edges := make([]workflow.Edge, 0, len(st.graph.Edges))
	for _, e := range st.graph.Edges {
		if e.Source != id && e.Target != id {
			edges = append(edges, e)
		}
	}
	st.graph.Edges = edges
	return fmt.Sprintf("remove_node %s (+ arestas incidentes)", id), nil
}

func (uc *aiBuilderUC) applyRemoveEdge(st *builderState, tc ai.ToolCall) (string, error) {
	source, _ := tc.Arguments["source"].(string)
	target, _ := tc.Arguments["target"].(string)
	label, hasLabel := tc.Arguments["label"].(string)
	edges := make([]workflow.Edge, 0, len(st.graph.Edges))
	removed := false
	for _, e := range st.graph.Edges {
		if e.Source == source && e.Target == target && (!hasLabel || e.Label == label) {
			removed = true
			continue
		}
		edges = append(edges, e)
	}
	st.graph.Edges = edges
	if !removed {
		return fmt.Sprintf("remove_edge %s→%s (já ausente)", source, target), nil
	}
	return fmt.Sprintf("remove_edge %s→%s", source, target), nil
}

func (uc *aiBuilderUC) validateNodeType(st *builderState, nt workflow.NodeType) error {
	if !nt.Valid() {
		return fmt.Errorf("tipo de nó %q inválido, use apenas tipos do catálogo", nt)
	}
	if nt.IsTrigger() {
		if workflow.TriggerType(nt).WorkflowType() != st.wfType {
			return fmt.Errorf("gatilho %q não é compatível com workflow do tipo %q", nt, st.wfType)
		}
		return nil
	}
	def, ok := workflow.NodeCatalogMap(st.fullCatalog)[nt]
	if !ok {
		return fmt.Errorf("tipo de nó %q não está disponível no catálogo", nt)
	}
	if !workflow.DefinitionAllowedForType(def, st.wfType) {
		return fmt.Errorf("tipo de nó %q não é permitido em workflows do tipo %q", nt, st.wfType)
	}
	return nil
}

func (uc *aiBuilderUC) freshNodeID(st *builderState) string {
	for {
		st.nextID++
		id := fmt.Sprintf("n%d", st.nextID)
		if st.graph.FindNode(id) == nil {
			return id
		}
	}
}

// ---- informational tools -------------------------------------------------

func (uc *aiBuilderUC) handleGetNodeSpec(st *builderState, tc ai.ToolCall) string {
	typeStr, _ := tc.Arguments["node_type"].(string)
	nt := workflow.NodeType(strings.TrimSpace(typeStr))
	def, ok := workflow.NodeCatalogMap(st.fullCatalog)[nt]
	if !ok {
		return fmt.Sprintf("get_node_spec(%s): tipo desconhecido", typeStr)
	}
	// Anti-dither: if the spec was already served this session, don't resend it,
	// push the model to act instead of re-inspecting (the classic ReAct loop).
	if st.inspectedSpecs[string(nt)] {
		return fmt.Sprintf("get_node_spec(%s): VOCÊ JÁ CONSULTOU ISTO. Pare de inspecionar e EXECUTE agora: use add_node/update_node para criar/configurar o nó e connect para ligá-lo. Não chame get_node_spec(%s) de novo.", nt, nt)
	}
	st.inspectedSpecs[string(nt)] = true
	var b strings.Builder
	fmt.Fprintf(&b, "get_node_spec(%s): %s, %s\n", nt, def.Label, def.Description)
	if len(def.ConfigSchema) > 0 {
		b.WriteString("  campos de config:\n")
		for _, f := range def.ConfigSchema {
			req := ""
			if f.Required {
				req = " [obrigatório]"
			}
			opts := ""
			if len(f.Options) > 0 {
				vals := make([]string, 0, len(f.Options))
				for _, o := range f.Options {
					vals = append(vals, o.Value)
				}
				opts = " opções=" + strings.Join(vals, "|")
			} else if f.OptionsSource != "" {
				opts = " (use find_resource " + f.OptionsSource + ")"
			}
			// Numeric range bounds, the AI MUST see Min/Max or it guesses (a "speed
			// no máximo" request becomes 2.0 when the real max is 1.2, the range
			// validator rejects it, and the agent dithers in a fix-up loop).
			rng := ""
			if f.Min != nil || f.Max != nil {
				lo, hi := "?", "?"
				if f.Min != nil {
					lo = fmt.Sprintf("%g", *f.Min)
				}
				if f.Max != nil {
					hi = fmt.Sprintf("%g", *f.Max)
				}
				rng = fmt.Sprintf(" intervalo=[%s, %s]", lo, hi)
				if f.Step != nil {
					rng += fmt.Sprintf(" passo=%g", *f.Step)
				}
			}
			fmt.Fprintf(&b, "    - %s (%s)%s%s%s\n", f.Key, f.Type, req, opts, rng)
		}
	}
	if len(def.Outputs) > 0 {
		b.WriteString("  saídas (handles):\n")
		for _, o := range def.Outputs {
			opt := ""
			if o.Optional {
				opt = " (opcional)"
			}
			fmt.Fprintf(&b, "    - %s%s\n", o.ID, opt)
		}
	}
	if len(def.OutputKeys) > 0 {
		keys := make([]string, 0, len(def.OutputKeys))
		for _, k := range def.OutputKeys {
			keys = append(keys, k.Key)
		}
		fmt.Fprintf(&b, "  chaves de saída p/ {{node.<id>.<chave>}}: %s\n", strings.Join(keys, ", "))
	}
	g := def.Guidance
	if g.When != "" {
		fmt.Fprintf(&b, "  quando usar: %s\n", g.When)
	}
	if g.Behavior != "" {
		fmt.Fprintf(&b, "  comportamento: %s\n", g.Behavior)
	}
	for _, ex := range g.Examples {
		fmt.Fprintf(&b, "  exemplo: %s\n", ex)
	}
	return b.String()
}

func (uc *aiBuilderUC) handleFindResource(ctx context.Context, emit agentloop.Emit, st *builderState, tc ai.ToolCall) string {
	kind, _ := tc.Arguments["kind"].(string)
	query, _ := tc.Arguments["query"].(string)
	limit := 5
	if l, ok := tc.Arguments["limit"].(float64); ok && int(l) > 0 && int(l) <= 25 {
		limit = int(l)
	}
	if uc.deps.ResourceResolver == nil {
		return fmt.Sprintf("find_resource(%s, %q): resolução de recursos indisponível nesta sessão", kind, query)
	}
	// Anti-dither: serve the same query from cache + tell the model to act.
	cacheKey := kind + ":" + strings.ToLower(strings.TrimSpace(query))
	if cached, ok := st.searchCache[cacheKey]; ok {
		return cached + ", VOCÊ JÁ BUSCOU ISTO. Use um dos ids acima AGORA (add_node/update_node); não repita find_resource para a mesma busca."
	}
	matches, err := uc.deps.ResourceResolver.Search(ctx, st.workspaceID, kind, query, limit)
	if err != nil {
		return fmt.Sprintf("find_resource(%s, %q): erro: %v", kind, query, err)
	}
	emit("resource_resolved", resourceResolvedPayload{Kind: kind, Query: query, Matches: matches})
	var result string
	if len(matches) == 0 {
		result = fmt.Sprintf("find_resource(%s, %q): NENHUM resultado, esse recurso não existe no workspace. NÃO repita esta busca. Construa o fluxo SEM ele (use outra abordagem/nó) ou, se for indispensável, finalize/responda perguntando ao usuário se deve criá-lo.", kind, query)
	} else {
		parts := make([]string, 0, len(matches))
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("%s=%s", m.ID, m.Name))
		}
		result = fmt.Sprintf("find_resource(%s, %q) → %s", kind, query, strings.Join(parts, "; "))
	}
	st.searchCache[cacheKey] = result
	return result
}

// ---- prompts -------------------------------------------------------------

func (uc *aiBuilderUC) systemPrompt(st *builderState) string {
	var b strings.Builder
	b.WriteString("Você é o Copiloto de Fluxos da ")
	b.WriteString(brand.Active().Name)
	b.WriteString(", um agente que CONSTRÓI e EDITA grafos de workflow (nós + arestas) a partir de pedidos em linguagem natural.\n\n")
	b.WriteString("Você opera por chamadas de ferramenta sobre um grafo mantido no servidor. A cada turno você recebe o estado atual do grafo e a lista de problemas do validador; faça mutações para resolver o pedido do usuário e DEIXAR O GRAFO VÁLIDO.\n\n")
	b.WriteString("REGRA DE TÉRMINO (inviolável): só chame finish quando NÃO houver problemas BLOQUEANTES. O validador do domínio é a única autoridade, finish é recusado se houver qualquer problema bloqueante. Problemas ADVISORY (dica) não impedem finish, mas resolva-os quando possível para um fluxo totalmente funcional. Nunca chame finish no mesmo turno em que faz mutações.\n\n")
	b.WriteString("REGRAS:\n")
	b.WriteString("- Para um workflow NOVO (sem nome ainda), defina um nome curto e uma descrição via set_meta logo no PRIMEIRO turno (junto das primeiras mutações).\n")
	b.WriteString("- Use apenas tipos de nó do catálogo abaixo. Todo workflow precisa de ao menos um gatilho (trigger) e ao menos um caminho até um nó 'end'.\n")
	b.WriteString("- O rótulo (label) de cada aresta deve ser o id exato de uma saída (handle) do nó de origem.\n")
	b.WriteString("- Nós de saída DINÂMICA (action_ai_agent em tool_mode=route → uma saída por custom_tool; condition_text_match → uma saída por cases[].value): defina a config (custom_tools/cases/tool_mode) ANTES de conectar as arestas a essas saídas.\n")
	b.WriteString("- O campo 'tool_mode' do action_ai_agent é OCULTO no schema mas essencial (route vs execute), veja get_node_spec.\n")
	b.WriteString("- Resolva ids de recursos (modelos, agentes, templates, departamentos, etiquetas, membros...) via find_resource; nunca invente ids.\n")
	b.WriteString("- MODELO DE IA (campo 'model' de action_ai_agent em modo prompt): use SEMPRE find_resource ai_models e copie um id EXATAMENTE como retornado (ex.: 'openai/gpt-4o', 'anthropic/claude-sonnet-4'). NUNCA escreva um id de memória nem invente variações/sufixos (ex.: '-latest', 'gpt-chat', 'gemini-3.5-flash'). Um modelo que não existe é REJEITADO na hora, não tente adivinhar; escolha um id da lista retornada.\n")
	b.WriteString("- Use get_node_spec(tipo) para ver o schema completo, saídas e orientações de um nó antes de configurá-lo.\n")
	b.WriteString("- Referencie dados de nós anteriores (ancestrais) com {{node.<id>.<chave>}}, variáveis com {{var.<nome>}}.\n")
	b.WriteString("- ORDEM TÍPICA DE CONSTRUÇÃO (siga e AJA, não fique só consultando): set_meta → add_node do gatilho → add_node dos nós de ação (ex.: action_ai_agent com source=prompt + model + instructions para conversar) → connect na ordem do fluxo → add_node 'end' e connect até ele → finish. Consulte get_node_spec/find_resource NO MÁXIMO uma vez por item; depois EXECUTE.\n")
	b.WriteString("- Para um atendente conversacional de IA: use action_ai_agent (source=prompt, model resolvido via find_resource ai_models, instructions com o tom/lógica desejada). Para conversa contínua, ligue ai_agent → wait_for_reply e a saída 'replied' de volta ao ai_agent, com 'timeout' indo para 'end'.\n\n")
	b.WriteString(workflow.VariableSystemGuide())
	b.WriteString("\n\n")

	b.WriteString("\n")

	fmt.Fprintf(&b, "TIPO DE WORKFLOW ATUAL: %s\n\n", st.wfType)
	b.WriteString("CATÁLOGO DE NÓS DISPONÍVEIS (cada item traz a orientação do próprio nó):\n")
	for _, line := range uc.catalogLines(st) {
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (uc *aiBuilderUC) catalogLines(st *builderState) []string {
	defs := uc.allowedDefs(st)
	lines := make([]string, 0, len(defs))
	for _, d := range defs {
		req := requiredFieldNames(d)
		reqStr := ""
		if len(req) > 0 {
			reqStr = " | obrigatórios: " + strings.Join(req, ",")
		}
		line := fmt.Sprintf("• %s | %s | %s | %s%s", d.Type, d.Label, truncate(d.Description, 90), d.Category, reqStr)
		// Surface each node's own guidance in the always-visible catalog so the
		// agent understands how every node works/behaves without an extra lookup.
		if g := d.Guidance; g.When != "" {
			line += "\n    quando: " + g.When
		}
		if b := d.Guidance.Behavior; b != "" {
			line += "\n    comportamento: " + b
		}
		// Surface the node's outputs DYNAMICALLY (the source of truth) so the AI
		// always knows the exits + the keys it can reference via {{node.<id>.<key>}}.
		if outs := staticHandleIDs(d.Outputs); outs != "" {
			line += "\n    saídas: " + outs
		}
		if keys := outputKeyNames(d.OutputKeys); keys != "" {
			line += "\n    chaves de saída ({{node.<id>.<chave>}}): " + keys
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}

// regroundMessage is the per-turn OBSERVATION of the graph.
//
// It must never restate the user's request. The request is anchored once at the
// head of the conversation; repeating it as the newest message every turn made
// the model read it as a question just asked, so it re-answered the same
// question on every iteration ("sim, o nó n5 continua sendo X") while the loop
// ground on. The same reasoning already applies to tool results, which are not
// re-stuffed here either.
func (uc *aiBuilderUC) regroundMessage(st *builderState, iter, maxIter, noMutationStreak int) string {
	var b strings.Builder
	b.WriteString("OBSERVAÇÃO DO SISTEMA (não é uma nova pergunta do usuário, não repita a resposta anterior):\n")
	fmt.Fprintf(&b, "ITERAÇÃO %d de %d (orçamento limitado).\n", iter, maxIter)
	// Escalating "act now" nudge, breaks the over-planning / repeated-lookup loop
	// that otherwise burns tokens without changing the graph.
	if noMutationStreak == 1 {
		b.WriteString("ATENÇÃO: você NÃO alterou o grafo no último turno. PARE de só consultar, execute AGORA uma mutação concreta (add_node/connect/update_node/remove_*) para resolver os problemas bloqueantes, ou chame finish se já estiver válido. Não repita get_node_spec/find_resource para algo que já consultou.\n")
	} else if noMutationStreak >= 2 {
		fmt.Fprintf(&b, "ATENÇÃO CRÍTICA: você está há %d turnos SEM alterar o grafo, pare de buscar/consultar imediatamente. Sua PRÓXIMA ação DEVE ser uma mutação que resolva os PROBLEMAS BLOQUEANTES listados abaixo (ex.: adicionar um nó 'end' e conectar a ele; conectar a saída obrigatória de um nó). Se um recurso (etiqueta, agente, etc.) NÃO existe no workspace, NÃO continue procurando: construa o fluxo sem ele (use outro nó) ou, se for indispensável, chame finish/responda perguntando ao usuário se deve criá-lo. NUNCA fique preso buscando algo que não existe.\n", noMutationStreak)
	}
	fmt.Fprintf(&b, "ESTADO ATUAL DO GRAFO (id=%q, tipo=%s, nome=%q):\n%s\n\n", st.workflowID, st.wfType, st.name, serializeGraph(st.graph))

	blocking := st.lastReport.Blocking()
	advisory := st.lastReport.Advisory()
	if len(blocking) == 0 {
		b.WriteString("PROBLEMAS BLOQUEANTES: nenhum. O grafo está VÁLIDO, se o pedido do usuário está atendido, chame finish AGORA.\n")
	} else {
		fmt.Fprintf(&b, "PROBLEMAS BLOQUEANTES (%d), devem ser resolvidos antes de finish:\n", len(blocking))
		for _, i := range blocking {
			fmt.Fprintf(&b, "  - [%s] %s%s\n", i.Code, i.Message, hintSuffix(i))
		}
	}
	if len(advisory) > 0 {
		fmt.Fprintf(&b, "\nDICAS (advisory, NÃO bloqueiam finish). Aplique no MÁXIMO uma vez cada. Se uma dica persistir depois de você já ter tentado corrigi-la, IGNORE-A e chame finish: insistir nela não torna o workflow válido, apenas gasta o orçamento:\n")
		for _, i := range advisory {
			fmt.Fprintf(&b, "  - [%s] %s%s\n", i.Code, i.Message, hintSuffix(i))
		}
	}
	// Tool results and the model's own prior actions are NOT re-stuffed here, they
	// live in the agentic message history (assistant tool calls + their RoleTool
	// results), which the model already sees. Duplicating them as prose wastes
	// tokens and is a documented cause of degraded tool-calling.
	b.WriteString("\nPRÓXIMO PASSO: faça as próximas mutações para resolver o pedido e zerar os problemas bloqueantes. Quando não houver bloqueantes e o pedido estiver atendido, chame finish (sozinho, sem outras ferramentas).")
	return b.String()
}

// ---- builder tools -------------------------------------------------------

func (uc *aiBuilderUC) builderTools(st *builderState) []tools.Definition {
	typeEnum := uc.typeEnum(st)
	str := func(desc string) tools.Parameter { return tools.Parameter{Type: "string", Description: desc} }
	enum := func(desc string, vals []string) tools.Parameter {
		return tools.Parameter{Type: "string", Description: desc, Enum: vals}
	}
	return []tools.Definition{
		{
			Name:        toolGetNodeSpec,
			Description: "Retorna o schema completo, saídas (handles) e orientações de uso de UM tipo de nó. Use antes de configurar nós que você não conhece bem.",
			Parameters:  map[string]tools.Parameter{"node_type": enum("tipo de nó do catálogo", typeEnum)},
			Required:    []string{"node_type"},
		},
		{
			Name:        toolFindResource,
			Description: "Resolve um nome humano de recurso para um id real do workspace (modelos de IA, agentes, templates, departamentos, etc.). Nunca invente ids, sempre resolva via esta ferramenta.",
			Parameters: map[string]tools.Parameter{
				"kind":  enum("tipo de recurso", resourceKinds),
				"query": str("texto de busca pelo nome do recurso"),
				"limit": {Type: "integer", Description: "máx. de resultados (1-25, padrão 5)"},
			},
			Required: []string{"kind", "query"},
		},
		{
			Name:        toolAddNode,
			Description: "Cria um nó e retorna seu id de servidor. config_json é um objeto JSON (como string) com a configuração do nó.",
			Parameters: map[string]tools.Parameter{
				"type":        enum("tipo do nó (catálogo)", typeEnum),
				"label":       str("rótulo opcional para exibição"),
				"config_json": str("configuração do nó como string JSON (objeto)"),
				"client_id":   str("id temporário opcional para idempotência em re-tentativas"),
			},
			Required: []string{"type"},
		},
		{
			Name:        toolConnect,
			Description: "Cria uma aresta direcionada de source→target. label deve ser o id de uma saída (handle) do nó de origem (ex.: 'true'/'false', 'replied'/'timeout', o nome de uma ferramenta ou o valor de um case).",
			Parameters: map[string]tools.Parameter{
				"source": str("id do nó de origem"),
				"target": str("id do nó de destino"),
				"label":  str("rótulo da aresta = id da saída do nó de origem (vazio para saída única)"),
			},
			Required: []string{"source", "target"},
		},
		{
			Name:        toolUpdateNode,
			Description: "Atualiza a configuração (mesclada) e/ou rótulo de um nó existente.",
			Parameters: map[string]tools.Parameter{
				"id":          str("id do nó"),
				"label":       str("novo rótulo opcional"),
				"config_json": str("config a mesclar, como string JSON (objeto)"),
			},
			Required: []string{"id"},
		},
		{
			Name:        toolRemoveNode,
			Description: "Remove um nó e todas as arestas incidentes. Idempotente.",
			Parameters:  map[string]tools.Parameter{"id": str("id do nó a remover")},
			Required:    []string{"id"},
		},
		{
			Name:        toolRemoveEdge,
			Description: "Remove uma aresta específica (source→target, opcionalmente filtrando por label). Idempotente.",
			Parameters: map[string]tools.Parameter{
				"source": str("id do nó de origem"),
				"target": str("id do nó de destino"),
				"label":  str("rótulo opcional para desambiguar"),
			},
			Required: []string{"source", "target"},
		},
		{
			Name:        toolSetMeta,
			Description: "Define metadados do workflow: nome e/ou descrição.",
			Parameters: map[string]tools.Parameter{
				"name":          str("nome do workflow"),
				"description":   str("descrição do workflow"),
				"workflow_type": enum("tipo do workflow", []string{string(workflow.WorkflowTypeMessages)}),
			},
		},
		{
			Name:        toolFinish,
			Description: "Solicita a conclusão. Só será aceito se NÃO houver problemas bloqueantes. Chame sozinho, sem outras ferramentas no mesmo turno.",
			Parameters:  map[string]tools.Parameter{"summary": str("resumo curto do que foi construído/alterado")},
			Required:    []string{"summary"},
		},
	}
}

func (uc *aiBuilderUC) typeEnum(st *builderState) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// All valid triggers for this workflow type (triggers are pass-through, may not
	// be executor-backed in the catalog).
	for _, tt := range allTriggerTypes() {
		if tt.WorkflowType() == st.wfType {
			add(string(tt))
		}
	}
	for _, d := range uc.allowedDefs(st) {
		add(string(d.Type))
	}
	sort.Strings(out)
	return out
}

func (uc *aiBuilderUC) allowedDefs(st *builderState) []workflow.NodeDefinition {
	var out []workflow.NodeDefinition
	for _, d := range st.fullCatalog {
		if d.Type.Category() == workflow.NodeCategoryDecoration {
			continue
		}
		if d.Type.IsTrigger() {
			if workflow.TriggerType(d.Type).WorkflowType() == st.wfType {
				out = append(out, d)
			}
			continue
		}
		if workflow.DefinitionAllowedForType(d, st.wfType) {
			out = append(out, d)
		}
	}
	return out
}

// ---- small helpers -------------------------------------------------------

func (st *builderState) pushLog(s string) {
	st.actionLog = append(st.actionLog, s)
	if len(st.actionLog) > builderLogTail {
		st.actionLog = st.actionLog[len(st.actionLog)-builderLogTail:]
	}
}

func (uc *aiBuilderUC) writeJSON(conn BuilderConn, mu *sync.Mutex, msg aiBuilderServerMsg) error {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(msg)
}

func (uc *aiBuilderUC) sendError(conn BuilderConn, mu *sync.Mutex, errMsg string) error {
	log.Printf("[workflow-ai-builder] error: %s", errMsg)
	return uc.writeJSON(conn, mu, aiBuilderServerMsg{Type: "error", Payload: map[string]string{"error": errMsg}})
}

func parseConfigJSON(raw interface{}) (map[string]interface{}, error) {
	if raw == nil {
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("config_json deve ser uma string JSON")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("config_json não é um objeto JSON válido: %v", err)
	}
	return m, nil
}

func serializeGraph(g *workflow.Graph) string {
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func graphHash(g *workflow.Graph) string {
	b, err := json.Marshal(g)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func blockingSignature(r workflow.LintReport) string {
	var keys []string
	for _, i := range r.Blocking() {
		keys = append(keys, string(i.Code)+":"+i.NodeID)
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

func hintSuffix(i workflow.LintIssue) string {
	parts := []string{}
	if i.NodeID != "" {
		parts = append(parts, "nó="+i.NodeID)
	}
	if i.Field != "" {
		parts = append(parts, "campo="+i.Field)
	}
	if i.Hint != "" {
		parts = append(parts, i.Hint)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

// staticHandleIDs renders a node's static output handles (id + optional flag)
// for the catalog. Dynamic-handle nodes (ai_agent route / text_match) have none
// here, their guidance explains the dynamic handles.
func staticHandleIDs(outs []workflow.HandleDefinition) string {
	if len(outs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(outs))
	for _, o := range outs {
		if o.Optional {
			parts = append(parts, o.ID+" (opcional)")
		} else {
			parts = append(parts, o.ID)
		}
	}
	return strings.Join(parts, ", ")
}

func outputKeyNames(keys []workflow.OutputKeyDefinition) string {
	if len(keys) == 0 {
		return ""
	}
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, k.Key)
	}
	return strings.Join(names, ", ")
}

func requiredFieldNames(d workflow.NodeDefinition) []string {
	var out []string
	for _, f := range d.ConfigSchema {
		if f.Required {
			out = append(out, f.Key)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func allTriggerTypes() []workflow.TriggerType {
	return []workflow.TriggerType{
		workflow.TriggerFirstMessage, workflow.TriggerMessageReceived, workflow.TriggerCampaignSent,
		workflow.TriggerStageAdded, workflow.TriggerManual, workflow.TriggerNoReply,
	}
}

// mutationSignature names WHAT a mutation acted on, for the engine's
// repeated-turn stall guard.
//
// Deliberately the tool name plus the target id, NOT the arguments: the failure
// this catches is a model re-editing one node over and over with slightly
// different config, chasing an advisory hint it cannot satisfy. Hashing the
// arguments would make each attempt look distinct and the guard would never
// fire, which is exactly what happened before it existed.
func mutationSignature(tc ai.ToolCall) string {
	for _, key := range []string{"node_id", "id", "edge_id", "from", "source"} {
		if v, ok := tc.Arguments[key].(string); ok && strings.TrimSpace(v) != "" {
			return tc.Name + ":" + v
		}
	}
	return tc.Name
}
