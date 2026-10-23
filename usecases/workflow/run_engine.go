package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/choicestats"
	"vozko/domain/metrics"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

var errNodePanic = errors.New("node executor panicked")

type NodeExecutorRegistry struct {
	executors   map[workflow.NodeType]workflow.NodeExecutor
	definitions map[workflow.NodeType]workflow.NodeDefinition
	catalogMu   sync.Mutex
	catalog     []workflow.NodeDefinition
}

func NewNodeExecutorRegistry() *NodeExecutorRegistry {
	return &NodeExecutorRegistry{
		executors:   make(map[workflow.NodeType]workflow.NodeExecutor),
		definitions: make(map[workflow.NodeType]workflow.NodeDefinition),
	}
}

func (r *NodeExecutorRegistry) Register(nodeType workflow.NodeType, executor workflow.NodeExecutor) {
	r.executors[nodeType] = executor
	if definer, ok := executor.(workflow.NodeDefiner); ok {
		r.define(definer.Definition())
	}
}

func (r *NodeExecutorRegistry) RegisterDefinition(def workflow.NodeDefinition) {
	r.define(def)
}

func (r *NodeExecutorRegistry) define(def workflow.NodeDefinition) {
	r.catalogMu.Lock()
	defer r.catalogMu.Unlock()
	r.definitions[def.Type] = workflow.NormalizeNodeDefinition(def)
	r.catalog = nil
}

func (r *NodeExecutorRegistry) Get(nodeType workflow.NodeType) (workflow.NodeExecutor, bool) {
	ex, ok := r.executors[nodeType.Canonical()]
	return ex, ok
}

func (r *NodeExecutorRegistry) Definition(nodeType workflow.NodeType) (workflow.NodeDefinition, bool) {
	def, ok := r.definitions[nodeType.Canonical()]
	return def, ok
}

func (r *NodeExecutorRegistry) StepName(nodeType workflow.NodeType) string {
	if def, ok := r.definitions[nodeType.Canonical()]; ok && def.Label != "" {
		return def.Label
	}
	return string(nodeType)
}

func (r *NodeExecutorRegistry) Catalog() []workflow.NodeDefinition {
	r.catalogMu.Lock()
	defer r.catalogMu.Unlock()
	if r.catalog == nil {
		r.catalog = make([]workflow.NodeDefinition, 0, len(r.definitions))
		for _, def := range r.definitions {
			r.catalog = append(r.catalog, def)
		}
	}
	return r.catalog
}

type RunEngine struct {
	runRepo        workflow.WorkflowRunRepository
	logRepo        workflow.WorkflowRunLogRepository
	registry       *NodeExecutorRegistry
	wakeScheduler  workflow.WakeScheduler
	runLocker      workflow.RunLocker
	automationGate workflow.AutomationGate
	metrics        metrics.WorkflowMetricsRecorder
	statuses       workflow.RunStatusReader
	agentRunEnds   workflow.AgentRunFinishedPublisher
	alerts         workflow.RuntimeAlertRecorder
	gate           workflow.StepGate
	choices        choicestats.Recorder
}

const runLockTTL = 5 * time.Minute

func NewRunEngine(
	runRepo workflow.WorkflowRunRepository,
	logRepo workflow.WorkflowRunLogRepository,
	registry *NodeExecutorRegistry,
) *RunEngine {
	return &RunEngine{
		runRepo:  runRepo,
		logRepo:  logRepo,
		registry: registry,
	}
}

func (e *RunEngine) SetWakeScheduler(scheduler workflow.WakeScheduler) {
	e.wakeScheduler = scheduler
}

func (e *RunEngine) SetRunLocker(locker workflow.RunLocker) {
	e.runLocker = locker
}

func (e *RunEngine) SetAutomationGate(gate workflow.AutomationGate) {
	e.automationGate = gate
}

func (e *RunEngine) SetMetrics(recorder metrics.WorkflowMetricsRecorder) {
	e.metrics = recorder
}

func (e *RunEngine) SetChoiceRecorder(recorder choicestats.Recorder) {
	e.choices = recorder
}

func (e *RunEngine) SetRunStatusReader(statuses workflow.RunStatusReader) {
	e.statuses = statuses
}

func (e *RunEngine) SetAgentRunFinishedPublisher(publisher workflow.AgentRunFinishedPublisher) {
	e.agentRunEnds = publisher
}

func (e *RunEngine) SetRuntimeAlerts(recorder workflow.RuntimeAlertRecorder) {
	e.alerts = recorder
}

func (e *RunEngine) SetStepGate(gate workflow.StepGate) {
	e.gate = gate
}

func (e *RunEngine) passGate(run *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
	gated, err := e.gate.BeforeStep(run, node)
	if err != nil {
		slog.Info("workflow.engine.gate_stopped", append(runLogAttrs(run, node.ID), slog.String("reason", err.Error()))...)
		run.SetCancelled()
		now := time.Now().UTC()
		e.writeStep(run, step{
			nodeID: node.ID, nodeType: node.Type, status: workflow.LogStatusCancelled, attempt: run.RetryCount + 1,
			started: now, finished: now, err: workflow.Fail(workflow.ErrorKindCancelled, err),
		})
		return nil, err
	}
	if gated == nil || gated.Result == nil {
		return nil, nil
	}
	return gated, nil
}

func (e *RunEngine) raiseAlert(signal workflow.RuntimeAlertSignal) {
	if e == nil || e.alerts == nil {
		return
	}
	if err := e.alerts.RecordRuntimeAlert(signal); err != nil {
		slog.Error("workflow.alerts.record_failed",
			slog.String("workspace_id", signal.Key.WorkspaceID), slog.String("workflow_id", signal.Key.WorkflowID),
			slog.String("kind", string(signal.Key.Kind)), slog.String("error", err.Error()))
	}
}

var errFinishedElsewhere = errors.New("the run was finished elsewhere (cancelled, completed or failed) while this step ran; it stopped here")

func (e *RunEngine) finishedElsewhere(run *workflow.WorkflowRun) error {
	if e.statuses == nil {
		return nil
	}
	status, err := e.statuses.RunStatus(run.ID)
	if err != nil {
		slog.Error("workflow.engine.status_unreadable", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		return fmt.Errorf("read status of run %s before the next step: %w", run.ID, err)
	}
	if status.IsTerminal() {
		return fmt.Errorf("run %s is %s: %w", run.ID, status, workflow.ErrRunTerminal)
	}
	return nil
}

func (e *RunEngine) stopFinishedElsewhere(run *workflow.WorkflowRun, cause error) {
	slog.Info("workflow.engine.finished_elsewhere", append(runLogAttrs(run, run.CurrentNodeID), slog.String("reason", cause.Error()))...)
	e.recordAtCurrentNode(run, workflow.LogStatusCancelled, run.RetryCount+1, workflow.Fail(workflow.ErrorKindCancelled, errFinishedElsewhere))
}

func (e *RunEngine) announceAgentRunEnd(run *workflow.WorkflowRun) {
	if e.agentRunEnds == nil {
		return
	}
	ev, ok := workflow.AgentRunFinishedOf(run)
	if !ok || ev.Continuation() == workflow.ContinuationNone {
		return
	}
	if err := e.agentRunEnds.PublishAgentRunFinished(ev); err != nil {
		slog.Error("workflow.engine.agent_run_end_publish_failed", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		if e.metrics != nil {
			e.metrics.IncWorkflowAgentContinuation("publish_failed")
		}
	}
}

type SandboxAutomationGate struct{}

func (SandboxAutomationGate) AutomationEnabled(string, string) (bool, error) { return true, nil }

var errNoAutomationGate = errors.New("no automation gate is wired")

func (e *RunEngine) automationOff(entryID, entryType string) (bool, error) {
	if entryID == "" {
		return false, nil
	}
	if e == nil || e.automationGate == nil {
		return false, fmt.Errorf("%w: entry %s (%s): %v", workflow.ErrAutomationStateUnreadable, entryID, entryType, errNoAutomationGate)
	}
	enabled, err := e.automationGate.AutomationEnabled(entryID, entryType)
	if err != nil {
		return false, fmt.Errorf("%w: entry %s (%s): %v", workflow.ErrAutomationStateUnreadable, entryID, entryType, err)
	}
	return !enabled, nil
}

func retryBackoff(attempt int) time.Duration {
	return time.Duration(attempt*attempt*5) * time.Second
}

func waitForRetry(run *workflow.WorkflowRun, attempt int, reason workflow.WaitReason) time.Duration {
	delay := retryBackoff(attempt)
	run.SetWaiting(time.Now().UTC().Add(delay).UnixMilli(), reason)
	return delay
}

func runLogAttrs(run *workflow.WorkflowRun, nodeID string) []any {
	return node_executors.RunLogAttrs(run, nodeID)
}

func (e *RunEngine) TryLockRun(runID string) bool {
	if e.runLocker == nil {
		return true
	}
	acquired, err := e.runLocker.TryLock(runID, runLockTTL)
	if err != nil {
		slog.Warn("workflow.engine.lock_unreadable_retry_later", slog.String("run_id", runID), slog.String("error", err.Error()))
		return false
	}
	return acquired
}

func (e *RunEngine) UnlockRun(runID string) {
	if e.runLocker == nil {
		return
	}
	if err := e.runLocker.Unlock(runID); err != nil {
		slog.Warn("workflow.engine.unlock_failed", slog.String("run_id", runID), slog.String("error", err.Error()))
	}
}

func loopBodyNodes(g *workflow.Graph) map[string]bool {
	body := make(map[string]bool)

	for _, n := range g.Nodes {
		if n.Type != workflow.NodeTypeActionLoop {
			continue
		}
		loopID := n.ID
		for _, edge := range g.OutgoingEdges(loopID) {
			if edge.Label != "body" {
				continue
			}

			visited := map[string]bool{loopID: true}
			queue := []string{edge.Target}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				if visited[cur] {
					continue
				}
				visited[cur] = true
				body[cur] = true
				for _, out := range g.OutgoingEdges(cur) {
					if !visited[out.Target] {
						queue = append(queue, out.Target)
					}
				}
			}
		}
	}
	return body
}

const maxAgentCycleRevisits = 50

func agentCycleNodes(g *workflow.Graph) map[string]bool {
	relaxed := make(map[string]bool)
	for _, n := range g.Nodes {
		if n.Type != workflow.NodeTypeActionAIAgent {
			continue
		}
		forward := reachableNodes(g, n.ID, true)
		backward := reachableNodes(g, n.ID, false)
		for id := range forward {
			if backward[id] {
				relaxed[id] = true
			}
		}
		relaxed[n.ID] = true
	}
	return relaxed
}

func reachableNodes(g *workflow.Graph, start string, forward bool) map[string]bool {
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if forward {
			for _, e := range g.OutgoingEdges(cur) {
				if !visited[e.Target] {
					visited[e.Target] = true
					queue = append(queue, e.Target)
				}
			}
			continue
		}
		for _, e := range g.IncomingEdges(cur) {
			if !visited[e.Source] {
				visited[e.Source] = true
				queue = append(queue, e.Source)
			}
		}
	}
	return visited
}

func (e *RunEngine) Execute(run *workflow.WorkflowRun, w *workflow.Workflow) error {
	return e.ExecuteWithRuntime(run, w, nil)
}

func (e *RunEngine) ExecuteWithRuntime(run *workflow.WorkflowRun, w *workflow.Workflow, runtime interface{}) error {
	err := e.execute(run, w, runtime)
	if errors.Is(err, workflow.ErrRunTerminal) {
		e.stopFinishedElsewhere(run, err)
		return nil
	}
	return err
}

func (e *RunEngine) save(run *workflow.WorkflowRun) error {
	return e.saveWith(e.runRepo, run)
}

func (e *RunEngine) saveWith(runs workflow.WorkflowRunRepository, run *workflow.WorkflowRun) error {
	if err := runs.Update(run); err != nil {
		return err
	}
	if e == nil || !run.Status.IsTerminal() {
		return nil
	}
	if e.metrics != nil {
		e.metrics.IncWorkflowRunFinished(string(run.Status))
	}
	e.announceAgentRunEnd(run)
	return nil
}

func (e *RunEngine) stopByGuard(run *workflow.WorkflowRun, nodeID string, nodeType workflow.NodeType, cause error) error {
	slog.Warn("workflow.engine.guard_stop", append(runLogAttrs(run, nodeID), slog.String("reason", cause.Error()))...)
	run.SetError(cause.Error())
	now := time.Now().UTC()
	e.writeStep(run, step{
		nodeID: nodeID, nodeType: nodeType, status: workflow.LogStatusFailed, attempt: run.RetryCount + 1,
		started: now, finished: now, err: workflow.Fail(workflow.ErrorKindGuard, cause),
	})
	return e.save(run)
}

func (e *RunEngine) execute(run *workflow.WorkflowRun, w *workflow.Workflow, runtime interface{}) error {
	guard := newStepGuard(&w.Graph, &run.State)
	if guard.firstStep() && e.metrics != nil {
		e.metrics.IncWorkflowRunStarted(string(run.TriggerType))
	}

	slog.Info("workflow.engine.execute", append(runLogAttrs(run, run.CurrentNodeID), slog.String("workflow_id", w.ID))...)

	for {
		if err := e.finishedElsewhere(run); err != nil {
			return err
		}

		if err := guard.full(); err != nil {
			return e.stopByGuard(run, run.CurrentNodeID, "", err)
		}

		node := w.Graph.FindNode(run.CurrentNodeID)
		if node == nil {
			return e.stopByGuard(run, run.CurrentNodeID, "", fmt.Errorf("node not found: %s", run.CurrentNodeID))
		}

		if workflow.CallEnded(runtime) {
			slog.Info("workflow.engine.caller_hung_up", runLogAttrs(run, node.ID)...)
			run.SetCancelled()
			now := time.Now().UTC()
			e.writeStep(run, step{
				nodeID: node.ID, nodeType: node.Type, status: workflow.LogStatusCancelled, attempt: run.RetryCount + 1,
				started: now, finished: now, err: workflow.Fail(workflow.ErrorKindCancelled, errors.New("the caller hung up before this node")),
			})
			return e.save(run)
		}

		if err := guard.admit(node); err != nil {
			return e.stopByGuard(run, node.ID, node.Type, err)
		}

		var served *workflow.GatedStep
		if e.gate != nil {
			gated, err := e.passGate(run, node)
			if err != nil {
				return e.save(run)
			}
			served = gated
		}

		if node.Type.IsEnd() {
			run.SetCompleted()
			now := time.Now().UTC()
			e.writeStep(run, step{nodeID: node.ID, nodeType: node.Type, status: workflow.LogStatusExecuted, attempt: 1, started: now, finished: now})
			return e.save(run)
		}

		executor, ok := e.registry.Get(node.Type)
		if !ok && served == nil {

			if node.Type.IsTrigger() {
				edges := w.Graph.OutgoingEdges(node.ID)
				if len(edges) == 0 {
					return e.stopByGuard(run, node.ID, node.Type, errors.New("trigger node has no outgoing edges"))
				}
				run.State.Set("_prev_node_id", node.ID)
				run.CurrentNodeID = edges[0].Target
				run.UpdatedAt = time.Now().UTC()
				now := time.Now().UTC()
				e.writeStep(run, step{
					nodeID: node.ID, nodeType: node.Type, status: workflow.LogStatusExecuted, attempt: 1,
					started: now, finished: now, branch: edges[0].Label,
				})
				guard.executed()
				if err := e.save(run); err != nil {
					return err
				}
				continue
			}
			return e.stopByGuard(run, node.ID, node.Type, workflow.ErrNodeExecutorNotFound)
		}

		trace := &workflow.StepTrace{}
		ctx := &workflow.NodeContext{
			Run:          run,
			Node:         node,
			Graph:        &w.Graph,
			Workflow:     w,
			State:        &run.State,
			Runtime:      runtime,
			Trace:        trace,
			EnsureActive: func() error { return e.finishedElsewhere(run) },
		}

		attempt := run.RetryCount + 1
		input := workflow.ResolvedConfig(node, &run.State)
		releaseScope := openInterruptScope(runtime, node)

		started := time.Now().UTC()
		var result *workflow.NodeResult
		var err error
		if served != nil {
			result = served.Result
		} else {
			result, err = safeExecute(executor, ctx)
		}
		finished := time.Now().UTC()
		releaseScope()
		if err := e.finishedElsewhere(run); err != nil {
			return err
		}
		guard.executed()
		if e.metrics != nil {
			e.metrics.ObserveWorkflowStepDuration(string(node.Type), finished.Sub(started))
		}
		ran := step{nodeID: node.ID, nodeType: node.Type, attempt: attempt, started: started, finished: finished, input: input, trace: trace, pinned: served != nil && served.Pinned}
		verdict, nextNodeID := judgeStep(&ran, &w.Graph, node, result, err, workflow.InCall(runtime))

		if verdict == verdictErrored {
			if ran.status == workflow.LogStatusCancelled {
				slog.Info("workflow.engine.node_cancelled", runLogAttrs(run, node.ID)...)
				run.SetCancelled()
				e.writeStep(run, ran)
				return e.save(run)
			}
			if errors.Is(err, errNodePanic) {
				slog.Error("workflow.engine.node_panicked", append(runLogAttrs(run, node.ID), slog.String("error", outbound.String(err.Error())))...)
				run.SetError(err.Error())
				e.writeStep(run, ran)
				return e.save(run)
			}
			permanent := workflow.IsPermanentRefusal(err)
			if run.RetryCount < workflow.MaxRetriesPerNode && !workflow.InCall(runtime) && !permanent {
				run.RetryCount++
				delay := waitForRetry(run, run.RetryCount, workflow.WaitReasonRetry)
				slog.Warn("workflow.engine.node_retrying", append(runLogAttrs(run, node.ID),
					slog.Int("attempt", run.RetryCount), slog.Int("max_retries", workflow.MaxRetriesPerNode),
					slog.Duration("delay", delay), slog.String("error", outbound.String(err.Error())))...)
				e.writeStep(run, ran)
				return e.save(run)
			}
			slog.Error("workflow.engine.node_failed", append(runLogAttrs(run, node.ID),
				slog.Int("retry", run.RetryCount), slog.Bool("in_call", workflow.InCall(runtime)), slog.Bool("permanent", permanent),
				slog.String("error", outbound.String(err.Error())))...)
			run.SetError(err.Error())
			e.writeStep(run, ran)
			return e.save(run)
		}

		run.RetryCount = 0

		if node.Type.WaitsForCaller() {
			guard.waited()
		}

		switch verdict {
		case verdictReturnedError:
			slog.Warn("workflow.engine.node_returned_error", append(runLogAttrs(run, node.ID), slog.String("error", outbound.String(result.Error)))...)
			run.SetError(result.Error)
			e.writeStep(run, ran)
			return e.save(run)
		case verdictCompleted:
			run.SetCompleted()
			e.writeStep(run, ran)
			return e.save(run)
		case verdictWaitDuringCall:
			slog.Warn("workflow.engine.wait_during_call", runLogAttrs(run, node.ID)...)
			run.SetError(workflow.ErrWaitDuringCall.Error())
			e.writeStep(run, ran)
			return e.save(run)
		}

		workflow.RecordNodeOutput(&run.State, node.ID, result.Output)

		switch verdict {
		case verdictWaiting:
			e.writeStep(run, ran)
			run.SetWaiting(result.Wait.WakeAt, result.Wait.Reason)
			if err := e.save(run); err != nil {
				return err
			}
			e.scheduleWake(run, node, result.Wait.WakeAt)
			return nil
		case verdictPathEnded:
			e.writeStep(run, ran)
			run.SetCompleted()
			return e.save(run)
		}

		e.writeStep(run, ran)

		run.State.Set("_prev_node_id", node.ID)
		run.CurrentNodeID = nextNodeID
		run.UpdatedAt = time.Now().UTC()

		if err := e.save(run); err != nil {
			return err
		}
	}
}

func resultBranchHint(result *workflow.NodeResult) string {
	if result.Branch != "" {
		return result.Branch
	}
	hint, _ := result.Output["next_edge"].(string)
	return hint
}

func routedFailure(result *workflow.NodeResult) (string, workflow.ErrorKind) {
	if result.ErrorKind == "" {
		return "", ""
	}
	if message, _ := result.Output["error"].(string); message != "" {
		return message, result.ErrorKind
	}
	return string(result.ErrorKind), result.ErrorKind
}

func (e *RunEngine) scheduleWake(run *workflow.WorkflowRun, node *workflow.Node, wakeAt int64) {
	if e.wakeScheduler == nil {
		return
	}
	err := e.wakeScheduler.ScheduleRunWake(run.ID, wakeAt)
	if err == nil {
		return
	}
	slog.Error("workflow.engine.wake_enqueue_failed", append(runLogAttrs(run, node.ID), slog.Int64("wake_at", wakeAt), slog.String("error", err.Error()))...)
	now := time.Now().UTC()
	e.writeStep(run, step{
		nodeID: node.ID, nodeType: node.Type, status: workflow.LogStatusWaiting, attempt: 1, started: now, finished: now,
		err: workflow.Fail(workflow.ErrorKindTransport, fmt.Errorf("the delayed wake could not be queued, the minute tick wakes the run instead: %w", err)),
	})
}

type stepVerdict int

const (
	verdictNext stepVerdict = iota
	verdictErrored
	verdictReturnedError
	verdictCompleted
	verdictWaitDuringCall
	verdictWaiting
	verdictPathEnded
)

func judgeStep(ran *step, g *workflow.Graph, node *workflow.Node, result *workflow.NodeResult, err error, inCall bool) (stepVerdict, string) {
	if err != nil {
		ran.status, ran.err = workflow.LogStatusFailed, err
		if errors.Is(err, context.Canceled) {
			ran.status = workflow.LogStatusCancelled
		}
		return verdictErrored, ""
	}
	ran.output = result.Output
	switch {
	case result.Error != "":
		ran.status, ran.message, ran.kind = workflow.LogStatusFailed, result.Error, result.ErrorKind
		return verdictReturnedError, ""
	case result.Complete:
		ran.status = workflow.LogStatusExecuted
		return verdictCompleted, ""
	case result.Wait != nil && inCall:
		ran.status, ran.err = workflow.LogStatusFailed, workflow.ErrWaitDuringCall
		return verdictWaitDuringCall, ""
	case result.Wait != nil:
		ran.status = workflow.LogStatusWaiting
		return verdictWaiting, ""
	}
	next := result.NextNodeID
	if next == "" {
		edges := g.OutgoingEdges(node.ID)
		if len(edges) == 0 {
			ran.status = workflow.LogStatusExecuted
			return verdictPathEnded, ""
		}
		next = edges[0].Target
	}
	ran.status = workflow.LogStatusExecuted
	if result.Skipped {
		ran.status = workflow.LogStatusSkipped
	}
	ran.branch = g.BranchLabel(node.ID, next, resultBranchHint(result))
	ran.message, ran.kind = routedFailure(result)
	if ran.message != "" {
		ran.status = workflow.LogStatusFailed
	}
	return verdictNext, next
}

func safeExecute(executor workflow.NodeExecutor, ctx *workflow.NodeContext) (result *workflow.NodeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			slog.Error("workflow.engine.panic_recovered", append(runLogAttrs(ctx.Run, ctx.Node.ID), slog.String("node_type", string(ctx.Node.Type)), slog.Any("panic", r))...)
			ctx.Trace.RecordStack(stack)
			result = nil
			err = workflow.Fail(workflow.ErrorKindPanic, fmt.Errorf("%w: node %s (%s): %v", errNodePanic, ctx.Node.ID, ctx.Node.Type, r))
		}
	}()
	result, err = executor.Execute(ctx)
	if err == nil && result == nil {
		err = fmt.Errorf("node %s (%s) returned no result", ctx.Node.ID, ctx.Node.Type)
	}
	return result, err
}

type step struct {
	nodeID   string
	nodeType workflow.NodeType
	status   workflow.LogStatus
	attempt  int
	started  time.Time
	finished time.Time
	input    map[string]interface{}
	output   map[string]interface{}
	branch   string
	err      error
	message  string
	kind     workflow.ErrorKind
	trace    *workflow.StepTrace
	pinned   bool
}

func (s step) failure() (string, []string, workflow.ErrorKind) {
	switch {
	case s.err != nil:
		kind := s.kind
		if kind == "" {
			kind = workflow.ClassifyError(s.err)
		}
		return workflow.StepErrorMessage(kind, s.err.Error()), workflow.ErrorChain(s.err), kind
	case s.message != "":
		kind := s.kind
		if kind == "" {
			kind = workflow.ErrorKindValidation
		}
		return s.message, []string{s.message}, kind
	}
	return "", nil, ""
}

func (e *RunEngine) writeStep(run *workflow.WorkflowRun, s step) {
	if e == nil || e.logRepo == nil {
		return
	}
	if err := e.logRepo.Create(storedStep(stepRow(run.ID, s), s.trace.Scrubbers()...)); err != nil {
		slog.Error("workflow.engine.step_write_failed", append(runLogAttrs(run, s.nodeID), slog.String("error", err.Error()))...)
	}
}

func stepRow(runID string, s step) *workflow.WorkflowRunLog {
	message, chain, kind := s.failure()
	started, finished := s.started, s.finished
	return &workflow.WorkflowRunLog{
		ID:             uuid.New().String(),
		RunID:          runID,
		NodeID:         s.nodeID,
		NodeType:       s.nodeType,
		Status:         s.status,
		Input:          s.input,
		Output:         s.output,
		Error:          message,
		ExecutedAt:     time.Now().UTC(),
		Attempt:        s.attempt,
		StartedAt:      &started,
		FinishedAt:     &finished,
		DurationMs:     workflow.StepDuration(started, finished),
		Branch:         s.branch,
		ErrorKind:      kind,
		ErrorChain:     chain,
		Stack:          s.trace.StackText(),
		ScriptLogs:     s.trace.Logs(),
		HTTP:           s.trace.Exchanges(),
		UnresolvedRefs: s.trace.Unresolved(),
		Pinned:         s.pinned,
	}
}

func (e *RunEngine) recordGate(run *workflow.WorkflowRun, status workflow.LogStatus, attempt int, cause error) {
	e.recordAtCurrentNode(run, status, attempt, workflow.Fail(workflow.ErrorKindGate, cause))
}

func (e *RunEngine) recordGuard(run *workflow.WorkflowRun, cause error) {
	e.recordAtCurrentNode(run, workflow.LogStatusFailed, run.RetryCount+1, workflow.Fail(workflow.ErrorKindGuard, cause))
}

func (e *RunEngine) recordAtCurrentNode(run *workflow.WorkflowRun, status workflow.LogStatus, attempt int, err error) {
	now := time.Now().UTC()
	e.writeStep(run, step{nodeID: run.CurrentNodeID, status: status, attempt: attempt, started: now, finished: now, err: err})
}
