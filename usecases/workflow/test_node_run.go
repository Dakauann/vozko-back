package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type TestWaitAnswer struct {
	Reply    string                 `json:"reply,omitempty"`
	OptionID string                 `json:"optionId,omitempty"`
	Form     map[string]interface{} `json:"form,omitempty"`
}

const (
	PathStopWaits    = "waits"
	PathStopFailed   = "failed"
	PathStopEnded    = "ended"
	PathStopDiverged = "diverged"
	PathStopLoop     = "loop"
	PathStopNoPath   = "no_path"
)

const (
	FixPinEdge          = "pin_edge"
	FixPinOutput        = "pin_output"
	FixMockAndRunDirect = "mock_and_run_direct"
	FixContinueWait     = "continue_wait"
)

type NodeTestFix struct {
	Kind     string   `json:"kind"`
	NodeID   string   `json:"node_id,omitempty"`
	Edge     string   `json:"edge,omitempty"`
	Needs    string   `json:"needs,omitempty"`
	MockKeys []string `json:"mock_keys,omitempty"`
}

type NodeTestPathStop struct {
	Reason          string                `json:"reason"`
	Message         string                `json:"message"`
	StoppedAt       string                `json:"stopped_at,omitempty"`
	StoppedAtType   workflow.NodeType     `json:"stopped_at_type,omitempty"`
	StoppedAtLabel  string                `json:"stopped_at_label,omitempty"`
	Wait            *workflow.WaitPreview `json:"wait,omitempty"`
	DivergedAt      string                `json:"diverged_at,omitempty"`
	DivergedAtType  workflow.NodeType     `json:"diverged_at_type,omitempty"`
	DivergedAtLabel string                `json:"diverged_at_label,omitempty"`
	TakenBranch     string                `json:"taken_branch"`
	ExpectedBranch  string                `json:"expected_branch"`
	Fixes           []NodeTestFix         `json:"fixes"`
}

type stepRun struct {
	result  *workflow.NodeResult
	err     error
	verdict stepVerdict
	next    string
	row     *workflow.WorkflowRunLog
}

func (r stepRun) failed() bool {
	switch r.verdict {
	case verdictErrored, verdictReturnedError, verdictWaitDuringCall:
		return true
	}
	return false
}

type nodeTest struct {
	wf       *workflow.Workflow
	target   *workflow.Node
	input    TestNodeInput
	run      *workflow.WorkflowRun
	registry *NodeExecutorRegistry
	runtime  interface{}
	done     chan struct{}
	effects  *simEffects

	mu       sync.Mutex
	upstream []*workflow.WorkflowRunLog
	current  *workflow.Node
	hops     []workflow.PathHop
}

func (uc *testNodeUseCase) executors() TestExecutors {
	if uc.deps.Executors != nil {
		return uc.deps.Executors
	}
	deps := uc.deps.ExecutorDeps
	return func(box SimSandbox) *NodeExecutorRegistry { return newSimulationRegistry(deps, box) }
}

func (uc *testNodeUseCase) timeout() time.Duration {
	if uc.deps.Timeout > 0 {
		return uc.deps.Timeout
	}
	return DefaultNodeTestTimeout
}

func (uc *testNodeUseCase) newNodeTest(wf *workflow.Workflow, node *workflow.Node, input TestNodeInput) (*nodeTest, error) {
	run := buildTestRun(wf, input.WorkspaceID)
	run.EntryType = string(shared.EntryTypeWhatsApp)
	messages := &simMessageRepo{}
	outbound := make(chan SimOutboundMessage, 256)
	effects := newSimEffects()
	confirmed := input.ConfirmRealCalls
	registry := uc.executors()(SimSandbox{
		WorkspaceID: input.WorkspaceID, LeadID: uuid.NewString(), Messages: messages, Outbound: outbound, Effects: effects,
		RealCallsConfirmed: func() bool { return confirmed },
	})
	if registry == nil {
		return nil, ErrNodeTestUnavailable
	}
	seedTriggerVars(&run.State, input.TriggerVars)
	seedSimulationConversation(messages, run, input.TriggerVars)
	t := &nodeTest{
		wf: wf, target: node, input: input, run: run, registry: registry,
		runtime: testVoiceRuntime(wf, input.MockedState),
		done:    make(chan struct{}),
		effects: effects,
	}
	go discardOutbound(outbound, t.done)
	return t, nil
}

func discardOutbound(outbound <-chan SimOutboundMessage, done <-chan struct{}) {
	for {
		select {
		case <-outbound:
		case <-done:
			return
		}
	}
}

func (t *nodeTest) setCurrent(node *workflow.Node) {
	t.mu.Lock()
	t.current = node
	t.mu.Unlock()
}

func (t *nodeTest) addUpstream(row *workflow.WorkflowRunLog) {
	t.mu.Lock()
	t.upstream = append(t.upstream, row)
	t.mu.Unlock()
}

func (t *nodeTest) snapshot() ([]*workflow.WorkflowRunLog, *workflow.Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*workflow.WorkflowRunLog(nil), t.upstream...), t.current
}

func (t *nodeTest) runStep(node *workflow.Node) stepRun {
	t.setCurrent(node)
	trace := &workflow.StepTrace{}
	ctx := &workflow.NodeContext{Run: t.run, Node: node, Graph: &t.wf.Graph, Workflow: t.wf, State: &t.run.State, Runtime: t.runtime, Trace: trace}
	input := workflow.ResolvedConfig(node, &t.run.State)
	started := time.Now().UTC()
	result, pinned, err := t.perform(ctx)
	finished := time.Now().UTC()
	ran := step{nodeID: node.ID, nodeType: node.Type, attempt: 1, started: started, finished: finished, input: input, trace: trace, pinned: pinned}
	verdict, next := judgeStep(&ran, &t.wf.Graph, node, result, err, workflow.InCall(t.runtime))
	row := sandboxStep(stepRow(t.run.ID, ran), t.effects.drain(), node.Pinned)
	return stepRun{result: result, err: err, verdict: verdict, next: next, row: storedStep(row, trace.Scrubbers()...)}
}

func (t *nodeTest) perform(ctx *workflow.NodeContext) (*workflow.NodeResult, bool, error) {
	node := ctx.Node
	if refusal := t.workflowTypeRefusal(node); refusal != nil {
		return refusal, false, nil
	}
	switch {
	case node.Type.IsTrigger():
		result, err := safeExecute(triggerTestExecutor{vars: t.input.TriggerVars}, ctx)
		return result, false, err
	case node.Type.IsEnd():
		return &workflow.NodeResult{Complete: true}, false, nil
	}
	if gated := testStepGate(&t.wf.Graph, node, node.Pinned, t.input.ConfirmRealCalls, &t.run.State); gated != nil {
		return gated.Result, gated.Pinned, nil
	}
	executor, ok := t.registry.Get(node.Type)
	if !ok {
		return nil, false, fmt.Errorf("%w: %s", workflow.ErrNodeExecutorNotFound, node.Type)
	}
	result, err := safeExecute(executor, ctx)
	return result, false, err
}

func (t *nodeTest) workflowTypeRefusal(node *workflow.Node) *workflow.NodeResult {
	if node.Type.IsTrigger() || node.Type.IsEnd() {
		return nil
	}
	def, ok := t.registry.Definition(node.Type)
	if !ok || len(def.Scopes) == 0 || workflow.DefinitionAllowedForType(def, t.wf.Type) {
		return nil
	}
	accepted := "fluxos de voz"
	if t.wf.Type == workflow.WorkflowTypeVoice {
		accepted = "fluxos de mensagens"
	}
	return &workflow.NodeResult{
		Error:     fmt.Sprintf("O nó %s (%s) só funciona em %s e não pode ser testado neste fluxo.", node.ID, t.label(node), accepted),
		ErrorKind: workflow.ErrorKindWrongWorkflowType,
	}
}

func (t *nodeTest) label(node *workflow.Node) string {
	return t.registry.StepName(node.Type)
}

type triggerTestExecutor struct {
	vars map[string]interface{}
}

func (e triggerTestExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{TestBehavior: workflow.NodeTestTrigger}
}

func (e triggerTestExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if ctx.Node.Type != workflow.NodeTypeTriggerAgentCall {
		output := make(map[string]interface{}, len(e.vars))
		for k, v := range e.vars {
			output[k] = v
		}
		return &workflow.NodeResult{Output: output}, nil
	}
	fields, err := workflow.AgentCallInputsOf(ctx.Node.Config)
	if err != nil {
		return nil, workflow.Fail(workflow.ErrorKindConfig, fmt.Errorf("%w: %v", workflow.ErrAgentCallInputsInvalid, err))
	}
	given, _ := e.vars[workflow.DataKeyAgentInput].(map[string]interface{})
	cleaned, missing, err := workflow.ValidateAgentCallInputs(fields, given)
	if err != nil {
		return &workflow.NodeResult{Error: "Campo enviado pelo agente com tipo errado: " + err.Error(), ErrorKind: workflow.ErrorKindValidation}, nil
	}
	if len(missing) > 0 {
		return &workflow.NodeResult{Error: "Faltam campos obrigatórios do agente: " + strings.Join(missing, ", "), ErrorKind: workflow.ErrorKindValidation}, nil
	}
	agent, _ := e.vars[workflow.DataKeyAgent].(map[string]interface{})
	agentID, _ := agent["id"].(string)
	agentName, _ := agent["name"].(string)
	data := workflow.AgentCallRunData(cleaned, agentID, agentName)
	for k, v := range data {
		ctx.State.Set(k, v)
	}
	return &workflow.NodeResult{Output: data}, nil
}

func (t *nodeTest) settle(out *TestNodeOutput, ran stepRun) {
	out.Step = ran.row
	out.Pinned = ran.row.Pinned
	if ran.failed() {
		out.Error, out.ErrorKind = ran.row.Error, ran.row.ErrorKind
		return
	}
	if ran.verdict == verdictWaiting {
		preview := workflow.PreviewWait(*ran.result.Wait, time.Now().UTC())
		out.Wait = &preview
	}
	out.Success = true
	out.ExecutionOutput = ran.result.Output
	workflow.RecordNodeOutput(&t.run.State, t.target.ID, ran.result.Output)
	out.StateAfter = copyState(t.run.State)
}

func (t *nodeTest) satisfyCaptureDeps(mockRoots map[string]bool) {
	g := &t.wf.Graph
	visited := map[string]bool{}
	var satisfy func(n *workflow.Node)
	satisfy = func(n *workflow.Node) {
		if n == nil || visited[n.ID] {
			return
		}
		visited[n.ID] = true
		for _, d := range workflow.ExtractDependencies(n.Config) {
			switch d.Scope {
			case "var", "sys", "last", "node", "ai":
				continue
			}
			varName := d.Scope
			if i := strings.IndexByte(varName, '['); i >= 0 {
				varName = varName[:i]
			}
			if varName == "" || mockRoots[varName] {
				continue
			}
			if _, ok := t.run.State.Get(varName); ok {
				continue
			}
			producer := findCaptureProducer(g, varName)
			if producer == nil || producer.ID == t.target.ID || nodeOutputMocked(producer, mockRoots) {
				continue
			}
			satisfy(producer)
			ran := t.runStep(producer)
			t.addUpstream(ran.row)
			if ran.err == nil && ran.result != nil {
				workflow.RecordNodeOutput(&t.run.State, producer.ID, ran.result.Output)
			}
		}
	}
	satisfy(t.target)
}

func (t *nodeTest) executeUntilTarget(ctx context.Context) *NodeTestPathStop {
	g := &t.wf.Graph
	targetID := t.target.ID
	ancestors := g.AncestorsOf(targetID)

	startNodeID := ""
	if trigger := g.TriggerNode(); trigger != nil && ancestors[trigger.ID] {
		startNodeID = trigger.ID
	} else {
		startNodeID = findAncestorRoot(ancestors, g)
	}
	if startNodeID == "" {
		return t.plainStop(PathStopNoPath, nil, fmt.Sprintf("Nenhum caminho do fluxo leva até %s.", targetID))
	}
	t.run.CurrentNodeID = startNodeID

	guard := newStepGuard(g, &t.run.State)

	for {
		if ctx.Err() != nil {
			return t.plainStop(PathStopFailed, nil, "O teste foi interrompido antes de chegar ao nó.")
		}
		if t.run.CurrentNodeID == targetID {
			return nil
		}
		node := g.FindNode(t.run.CurrentNodeID)
		if node == nil {
			return t.plainStop(PathStopNoPath, nil, fmt.Sprintf("O caminho até %s chegou a um nó que não existe: %s.", targetID, t.run.CurrentNodeID))
		}
		if err := guard.full(); err != nil {
			return t.plainStop(PathStopLoop, node, fmt.Sprintf("O caminho até %s passou do limite de %d etapas.", targetID, workflow.MaxExecutionsPerRun))
		}
		if err := guard.admit(node); err != nil {
			return t.plainStop(PathStopLoop, node, t.guardMessage(err, node))
		}

		ran := t.runStep(node)
		t.addUpstream(ran.row)
		guard.executed()

		switch {
		case ran.failed():
			return t.stopAt(PathStopFailed, node, nil, ran.row.Error)
		case ran.verdict == verdictCompleted || ran.verdict == verdictPathEnded:
			return t.stopAt(PathStopEnded, node, nil, "")
		case ran.verdict == verdictWaiting:
			workflow.RecordNodeOutput(&t.run.State, node.ID, ran.result.Output)
			preview := workflow.PreviewWait(*ran.result.Wait, time.Now().UTC())
			next, ok := t.continueWait(node)
			if !ok {
				return t.stopAt(PathStopWaits, node, &preview, "")
			}
			guard.waited()
			t.hop(node.ID, next, g.BranchLabel(node.ID, next, ""))
			continue
		}

		workflow.RecordNodeOutput(&t.run.State, node.ID, ran.result.Output)
		next, branch := ran.next, ran.row.Branch
		if next != targetID && !ancestors[next] {
			if ran.result.NextNodeID != "" && !node.Type.IsTrigger() {
				t.addHop(node.ID, next, branch)
				return t.stopAt(PathStopDiverged, g.FindNode(next), nil, "")
			}
			next = pickAncestorEdge(g.OutgoingEdges(node.ID), ancestors, targetID)
			branch = g.BranchLabel(node.ID, next, "")
		}
		if next == "" {
			return t.stopAt(PathStopEnded, node, nil, "")
		}
		t.hop(node.ID, next, branch)
	}
}

func (t *nodeTest) guardMessage(err error, node *workflow.Node) string {
	if errors.Is(err, workflow.ErrDurableExecutionsReached) {
		return fmt.Sprintf("O caminho até %s passou do limite de %d etapas da execução.", t.target.ID, workflow.MaxDurableExecutionsPerRun)
	}
	return fmt.Sprintf("O caminho até %s repetiu %s (%s) sem avançar.", t.target.ID, node.ID, t.label(node))
}

func (t *nodeTest) addHop(from, next, branch string) {
	t.hops = append(t.hops, workflow.PathHop{NodeID: from, Next: next, Branch: branch})
}

func (t *nodeTest) hop(from, next, branch string) {
	t.addHop(from, next, branch)
	t.run.State.Set("_prev_node_id", from)
	t.run.CurrentNodeID = next
}

func (t *nodeTest) continueWait(node *workflow.Node) (string, bool) {
	if !t.input.ContinueWaits {
		return "", false
	}
	answer := t.input.WaitAnswers[node.ID]
	switch {
	case node.Type == workflow.NodeTypeWaitDuration || node.Type == workflow.NodeTypeWaitSchedule:
		next, err := timerElapsedEdge(t.run, t.wf, node)
		if err != nil {
			return "", false
		}
		return next, true
	case node.Type.IsFormPrompt():
		if answer.Form == nil || AdvanceOnForm(t.run, t.wf, answer.Form) != nil {
			return "", false
		}
	case node.Type.ParksForReply():
		if answer.Reply == "" && answer.OptionID == "" {
			return "", false
		}
		data := map[string]interface{}{"message": answer.Reply}
		if answer.OptionID != "" {
			data["selected_option_id"] = answer.OptionID
		}
		if AdvanceOnReply(t.run, t.wf, data) != nil {
			return "", false
		}
	default:
		return "", false
	}
	return t.run.CurrentNodeID, true
}

func branchPhrase(label string) string {
	if label == "" {
		return "a saída padrão"
	}
	return label
}

func waitNeeds(node *workflow.Node) (string, bool) {
	switch {
	case node.Type == workflow.NodeTypeWaitDuration || node.Type == workflow.NodeTypeWaitSchedule:
		return "", true
	case node.Type.IsFormPrompt():
		return "form", true
	case node.Type.ParksForReply():
		return "reply", true
	}
	return "", false
}

func (t *nodeTest) plainStop(reason string, node *workflow.Node, message string) *NodeTestPathStop {
	stop := &NodeTestPathStop{Reason: reason, Message: message}
	if node != nil {
		stop.StoppedAt, stop.StoppedAtType, stop.StoppedAtLabel = node.ID, node.Type, t.label(node)
	}
	stop.Fixes = append(stop.Fixes, t.directFix())
	return stop
}

func (t *nodeTest) stopAt(reason string, node *workflow.Node, wait *workflow.WaitPreview, failure string) *NodeTestPathStop {
	g := &t.wf.Graph
	targetID := t.target.ID
	stop := &NodeTestPathStop{Reason: reason, StoppedAt: node.ID, StoppedAtType: node.Type, StoppedAtLabel: t.label(node), Wait: wait}
	where := fmt.Sprintf("O caminho até %s parou em %s (%s)", targetID, node.ID, stop.StoppedAtLabel)
	if reason == PathStopEnded {
		where = fmt.Sprintf("O caminho até %s terminou em %s (%s) sem chegar ao nó", targetID, node.ID, stop.StoppedAtLabel)
	}
	if reason == PathStopDiverged {
		where = fmt.Sprintf("O caminho até %s saiu para %s (%s), que não leva ao nó", targetID, node.ID, stop.StoppedAtLabel)
	}
	detail := ""
	if reason != PathStopFailed {
		if hop, edge, ok := workflow.Divergence(g, t.hops, targetID, node.ID); ok {
			diverged := g.FindNode(hop.NodeID)
			stop.DivergedAt, stop.DivergedAtType, stop.DivergedAtLabel = diverged.ID, diverged.Type, t.label(diverged)
			stop.TakenBranch, stop.ExpectedBranch = hop.Branch, edge.Label
			detail = fmt.Sprintf("%s (%s) seguiu %s, não %s", diverged.ID, stop.DivergedAtLabel, branchPhrase(hop.Branch), branchPhrase(edge.Label))
			stop.Fixes = append(stop.Fixes, NodeTestFix{Kind: FixPinEdge, NodeID: diverged.ID, Edge: edge.Label})
		}
	}
	switch {
	case reason == PathStopFailed:
		detail = failure
		stop.Fixes = append(stop.Fixes, NodeTestFix{Kind: FixPinOutput, NodeID: node.ID})
	case reason == PathStopWaits && detail == "" && wait != nil:
		detail = "o nó " + wait.Description
	}
	if reason == PathStopWaits {
		if needs, ok := waitNeeds(node); ok {
			stop.Fixes = append(stop.Fixes, NodeTestFix{Kind: FixContinueWait, NodeID: node.ID, Needs: needs})
		}
	}
	stop.Message = where + "."
	if detail = strings.TrimRight(strings.TrimSpace(detail), "."); detail != "" {
		stop.Message = where + ": " + detail + "."
	}
	stop.Fixes = append(stop.Fixes, t.directFix())
	return stop
}

func (t *nodeTest) directFix() NodeTestFix {
	keys := []string{}
	for _, m := range workflow.GetRequiredMocks(workflow.ExtractDependencies(t.target.Config)) {
		keys = append(keys, m.StateKey)
	}
	return NodeTestFix{Kind: FixMockAndRunDirect, NodeID: t.target.ID, MockKeys: keys}
}

var errNodeTestTimedOut = errors.New("the node test ran past its time limit")

func (t *nodeTest) abortedRow(node *workflow.Node, err error, stack string) *workflow.WorkflowRunLog {
	trace := &workflow.StepTrace{}
	if stack != "" {
		trace.RecordStack(stack)
	}
	now := time.Now().UTC()
	ran := step{nodeID: node.ID, nodeType: node.Type, status: workflow.LogStatusFailed, attempt: 1, started: now, finished: now, err: err, trace: trace}
	return storedStep(stepRow(t.run.ID, ran), trace.Scrubbers()...)
}

func (t *nodeTest) aborted(kind workflow.ErrorKind, message string, err error, stack string) *TestNodeOutput {
	upstream, current := t.snapshot()
	out := &TestNodeOutput{NodeID: t.target.ID, NodeType: t.target.Type, Error: message, ErrorKind: kind, UpstreamSteps: upstream}
	if current == nil {
		current = t.target
	}
	row := t.abortedRow(current, workflow.Fail(kind, err), stack)
	if current.ID == t.target.ID {
		out.Step = row
	} else {
		out.UpstreamSteps = append(out.UpstreamSteps, row)
	}
	return out
}

func (t *nodeTest) timedOut(ctx context.Context, limit time.Duration) *TestNodeOutput {
	if errors.Is(ctx.Err(), context.Canceled) {
		return t.aborted(workflow.ErrorKindCancelled, "O teste foi cancelado antes de terminar.", context.Canceled, "")
	}
	return t.aborted(workflow.ErrorKindTimeout, fmt.Sprintf("O teste passou do limite de %d s e foi interrompido.", int(limit/time.Second)), errNodeTestTimedOut, "")
}

func (t *nodeTest) crashed(r interface{}) *TestNodeOutput {
	return t.aborted(workflow.ErrorKindPanic, "O teste falhou com um erro interno.", fmt.Errorf("%w: %v", errNodePanic, r), string(debug.Stack()))
}
