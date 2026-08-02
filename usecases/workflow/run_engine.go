package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflow"
)

// errNodePanic wraps a panic recovered while executing a node. It is terminal:
// a panic is a programming error (e.g. a nil dependency), not a transient
// failure, so the run fails fast instead of retrying — and, crucially, it never
// escapes to crash the HTTP serving goroutine.
var errNodePanic = errors.New("node executor panicked")

type NodeExecutorRegistry struct {
	executors   map[workflow.NodeType]workflow.NodeExecutor
	definitions map[workflow.NodeType]workflow.NodeDefinition
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
		r.definitions[nodeType] = workflow.NormalizeNodeDefinition(definer.Definition())
	}
}

func (r *NodeExecutorRegistry) RegisterDefinition(def workflow.NodeDefinition) {
	r.definitions[def.Type] = workflow.NormalizeNodeDefinition(def)
}

// Get resolves the executor for a node type, mapping retired wire values
// forward.
//
// Graph decoding already normalizes, so this is belt-and-braces for any path
// that builds a Node in Go without going through JSON (tests, fixtures, the
// simulator). A missing executor stops a run dead, so it is worth the one
// method call.
func (r *NodeExecutorRegistry) Get(nodeType workflow.NodeType) (workflow.NodeExecutor, bool) {
	ex, ok := r.executors[nodeType.Canonical()]
	return ex, ok
}

func (r *NodeExecutorRegistry) Catalog() []workflow.NodeDefinition {
	defs := make([]workflow.NodeDefinition, 0, len(r.definitions))
	for _, def := range r.definitions {
		defs = append(defs, workflow.NormalizeNodeDefinition(def))
	}
	return defs
}

type RunEngine struct {
	runRepo       workflow.WorkflowRunRepository
	logRepo       workflow.WorkflowRunLogRepository
	registry      *NodeExecutorRegistry
	wakeScheduler workflow.WakeScheduler
	runLocker     workflow.RunLocker
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

func (e *RunEngine) TryLockRun(runID string) bool {
	if e.runLocker == nil {
		return true
	}
	acquired, err := e.runLocker.TryLock(runID, runLockTTL)
	if err != nil {
		log.Printf("[workflow] engine: failed to acquire lock for run=%s: %v (proceeding unlocked)", runID, err)
		return true
	}
	return acquired
}

func (e *RunEngine) UnlockRun(runID string) {
	if e.runLocker == nil {
		return
	}
	if err := e.runLocker.Unlock(runID); err != nil {
		log.Printf("[workflow] engine: failed to release lock for run=%s: %v", runID, err)
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

// agentCycleNodes returns the nodes that sit on a cycle running through an
// AI-agent node — the agent "hub" plus the tool/action nodes it routes to that
// route back to it. An AI agent legitimately revisits its hub once per tool call
// within a single uninterrupted pass (agent → http → agent → http → …), which is
// forward progress, not an infinite loop. These nodes get a relaxed per-node
// revisit limit, like explicit loop bodies, so multi-tool turns aren't flagged as
// loops. A node is on such a cycle iff it is both reachable from the agent
// (downstream) and able to reach the agent (upstream).
func agentCycleNodes(g *workflow.Graph) map[string]bool {
	relaxed := make(map[string]bool)
	for _, n := range g.Nodes {
		if n.Type != workflow.NodeTypeActionAIAgent {
			continue
		}
		forward := reachableNodes(g, n.ID, true)   // downstream of the agent
		backward := reachableNodes(g, n.ID, false) // can reach the agent
		for id := range forward {
			if backward[id] {
				relaxed[id] = true
			}
		}
		relaxed[n.ID] = true
	}
	return relaxed
}

// reachableNodes does a BFS from start. forward=true follows outgoing edges
// (downstream); forward=false follows incoming edges (upstream). start is included.
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
	return e.execute(run, w, nil)
}

func (e *RunEngine) ExecuteWithRuntime(run *workflow.WorkflowRun, w *workflow.Workflow, runtime interface{}) error {
	return e.execute(run, w, runtime)
}

func (e *RunEngine) execute(run *workflow.WorkflowRun, w *workflow.Workflow, runtime interface{}) error {
	executionCount := 0

	// durableSteps is the lifetime node-execution count, loaded from persisted
	// run state so it SURVIVES waits (executionCount/nodeVisitCounts below are
	// per-pass and reset on every re-entry). This is the backstop that catches a
	// loop cycling through a wait node — see MaxDurableExecutionsPerRun.
	durableSteps := run.State.GetInt(workflow.StateKeyDurableSteps)

	nodeVisitCounts := make(map[string]int)
	const maxNodeRevisits = 3
	insideLoop := loopBodyNodes(&w.Graph)
	agentCycle := agentCycleNodes(&w.Graph)

	log.Printf("[workflow] engine: starting execution run=%s workflow=%s entry=%s", run.ID, w.ID, run.EntryID)

	for {
		if executionCount >= workflow.MaxExecutionsPerRun {
			run.SetError(workflow.ErrMaxExecutionsReached.Error())
			return e.runRepo.Update(run)
		}

		node := w.Graph.FindNode(run.CurrentNodeID)
		if node == nil {
			run.SetError(fmt.Sprintf("node not found: %s", run.CurrentNodeID))
			return e.runRepo.Update(run)
		}

		// Durable lifetime circuit breaker: counts every node visit and persists
		// across waits, so a cycle through a wait node (which resets the per-pass
		// counters above) still trips a hard stop instead of running forever.
		durableSteps++
		run.State.Set(workflow.StateKeyDurableSteps, durableSteps)
		if durableSteps > workflow.MaxDurableExecutionsPerRun {
			log.Printf("[workflow] engine: run=%s DURABLE LOOP CIRCUIT BREAKER — %d lifetime node executions across waits, stopping",
				run.ID, durableSteps)
			run.SetError(workflow.ErrDurableExecutionsReached.Error())
			return e.runRepo.Update(run)
		}

		nodeVisitCounts[node.ID]++
		revisitLimit := maxNodeRevisits
		if node.Type == workflow.NodeTypeActionLoop || insideLoop[node.ID] {
			revisitLimit = workflow.MaxExecutionsPerRun
		} else if agentCycle[node.ID] {
			// AI-agent hub (and the tool nodes it routes to) — revisited once per
			// tool call in a single pass, which is progress, not a loop. Allow many
			// tool cycles but still cap to catch a genuinely stuck agent.
			revisitLimit = maxAgentCycleRevisits
		}
		if nodeVisitCounts[node.ID] > revisitLimit {
			log.Printf("[workflow] engine: run=%s LOOP DETECTED — node=%s visited %d times, stopping",
				run.ID, node.ID, nodeVisitCounts[node.ID])
			run.SetError(fmt.Sprintf("loop detected: node %s visited %d times without waiting", node.ID, nodeVisitCounts[node.ID]))
			return e.runRepo.Update(run)
		}

		if node.Type.IsEnd() {
			log.Printf("[workflow] engine: run=%s reached end node=%s — completing", run.ID, node.ID)
			run.SetCompleted()
			e.writeLog(run.ID, node, workflow.LogStatusExecuted, nil, nil, "")
			return e.runRepo.Update(run)
		}

		executor, ok := e.registry.Get(node.Type)
		if !ok {

			if node.Type.IsTrigger() {
				log.Printf("[workflow] engine: run=%s trigger node=%s type=%s — passing through", run.ID, node.ID, node.Type)
				edges := w.Graph.OutgoingEdges(node.ID)
				if len(edges) == 0 {
					run.SetError("trigger node has no outgoing edges")
					return e.runRepo.Update(run)
				}
				run.State.Set("_prev_node_id", node.ID)
				run.CurrentNodeID = edges[0].Target
				run.UpdatedAt = time.Now().UTC()
				e.writeLog(run.ID, node, workflow.LogStatusExecuted, nil, nil, "")
				executionCount++
				if err := e.runRepo.Update(run); err != nil {
					return err
				}
				continue
			}
			run.SetError(workflow.ErrNodeExecutorNotFound.Error())
			return e.runRepo.Update(run)
		}

		ctx := &workflow.NodeContext{
			Run:      run,
			Node:     node,
			Graph:    &w.Graph,
			Workflow: w,
			State:    &run.State,
			Runtime:  runtime,
		}

		releaseScope := openInterruptScope(runtime, node)

		log.Printf("[workflow] engine: run=%s executing node=%s type=%s step=%d", run.ID, node.ID, node.Type, executionCount+1)
		result, err := e.safeExecute(executor, ctx)
		releaseScope()
		executionCount++

		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Printf("[workflow] engine: run=%s node=%s cancelled", run.ID, node.ID)
				run.SetCancelled()
				e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, nil, err.Error())
				return e.runRepo.Update(run)
			}
			if errors.Is(err, errNodePanic) {
				// Terminal: don't retry a panic, and never let it crash the server.
				log.Printf("[workflow] engine: run=%s node=%s PANICKED — failing run: %v", run.ID, node.ID, err)
				run.SetError(err.Error())
				e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, nil, err.Error())
				return e.runRepo.Update(run)
			}
			if run.RetryCount < workflow.MaxRetriesPerNode {
				run.RetryCount++
				retryDelay := int64(run.RetryCount*run.RetryCount*5) * int64(time.Second)
				run.SetWaiting(time.Now().UTC().UnixMilli()+retryDelay/int64(time.Millisecond), workflow.WaitReasonRetry)
				log.Printf("[workflow] engine: run=%s node=%s RETRYING (%d/%d) in %ds — error: %v",
					run.ID, node.ID, run.RetryCount, workflow.MaxRetriesPerNode, run.RetryCount*run.RetryCount*5, err)
				e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, nil, err.Error())
				return e.runRepo.Update(run)
			}
			log.Printf("[workflow] engine: run=%s node=%s FAILED (retries exhausted) — error: %v",
				run.ID, node.ID, err)
			run.SetError(err.Error())
			e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, nil, err.Error())
			return e.runRepo.Update(run)
		}

		run.RetryCount = 0

		if result.Error != "" {
			log.Printf("[workflow] engine: run=%s node=%s returned error: %s", run.ID, node.ID, result.Error)
			run.SetError(result.Error)
			e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, result.Output, result.Error)
			return e.runRepo.Update(run)
		}

		if result.Complete {
			log.Printf("[workflow] engine: run=%s node=%s signaled completion", run.ID, node.ID)
			run.SetCompleted()
			e.writeLog(run.ID, node, workflow.LogStatusExecuted, nil, result.Output, "")
			return e.runRepo.Update(run)
		}

		if result.Output != nil {
			for k, v := range result.Output {
				run.State.Set("_last_"+k, v)
			}

			for k, v := range result.Output {
				run.State.Set("_node_"+node.ID+"_"+k, v)
			}
		}

		e.writeLog(run.ID, node, workflow.LogStatusExecuted, nil, result.Output, "")

		if result.Wait != nil {
			log.Printf("[workflow] engine: run=%s node=%s waiting reason=%s until=%d", run.ID, node.ID, result.Wait.Reason, result.Wait.WakeAt)
			run.SetWaiting(result.Wait.WakeAt, result.Wait.Reason)
			if err := e.runRepo.Update(run); err != nil {
				return err
			}
			if e.wakeScheduler != nil {
				if err := e.wakeScheduler.ScheduleRunWake(run.ID, result.Wait.WakeAt); err != nil {

					log.Printf("[workflow] engine: failed to enqueue delayed wake for run=%s wakeAt=%d: %v", run.ID, result.Wait.WakeAt, err)
				}
			}
			return nil
		}

		nextNodeID := result.NextNodeID
		if nextNodeID == "" {
			edges := w.Graph.OutgoingEdges(node.ID)
			if len(edges) == 0 {
				run.SetCompleted()
				return e.runRepo.Update(run)
			}
			nextNodeID = edges[0].Target
		}

		run.State.Set("_prev_node_id", node.ID)
		run.CurrentNodeID = nextNodeID
		run.UpdatedAt = time.Now().UTC()

		if err := e.runRepo.Update(run); err != nil {
			return err
		}
	}
}

// safeExecute runs a node executor with a panic barrier. A misconfigured or
// buggy executor (e.g. a nil dependency, as happens for repo-backed nodes in the
// simulation registry) must never propagate a panic up through the HTTP handler
// and kill the serving goroutine — it is converted into a terminal node error.
// It also defends against an executor returning (nil, nil), which would nil-deref
// downstream.
func (e *RunEngine) safeExecute(executor workflow.NodeExecutor, ctx *workflow.NodeContext) (result *workflow.NodeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[workflow] engine: PANIC recovered in node=%s type=%s: %v\n%s", ctx.Node.ID, ctx.Node.Type, r, debug.Stack())
			result = nil
			err = fmt.Errorf("%w: node %s (%s): %v", errNodePanic, ctx.Node.ID, ctx.Node.Type, r)
		}
	}()
	result, err = executor.Execute(ctx)
	if err == nil && result == nil {
		err = fmt.Errorf("node %s (%s) returned no result", ctx.Node.ID, ctx.Node.Type)
	}
	return result, err
}

func (e *RunEngine) writeLog(runID string, node *workflow.Node, status workflow.LogStatus, input, output map[string]interface{}, errMsg string) {
	logEntry := &workflow.WorkflowRunLog{
		ID:         uuid.New().String(),
		RunID:      runID,
		NodeID:     node.ID,
		NodeType:   node.Type,
		Status:     status,
		Input:      input,
		Output:     output,
		Error:      errMsg,
		ExecutedAt: time.Now().UTC(),
	}
	if err := e.logRepo.Create(logEntry); err != nil {
		log.Printf("[workflow] failed to write run log: %v", err)
	}
}
