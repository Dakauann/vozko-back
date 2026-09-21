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
	runRepo        workflow.WorkflowRunRepository
	logRepo        workflow.WorkflowRunLogRepository
	registry       *NodeExecutorRegistry
	wakeScheduler  workflow.WakeScheduler
	runLocker      workflow.RunLocker
	automationGate workflow.AutomationGate
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

func (e *RunEngine) automationOff(entryID, entryType string) bool {
	if e == nil || e.automationGate == nil || entryID == "" {
		return false
	}
	return !e.automationGate.AutomationEnabled(entryID, entryType)
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
	return e.execute(run, w, nil)
}

func (e *RunEngine) ExecuteWithRuntime(run *workflow.WorkflowRun, w *workflow.Workflow, runtime interface{}) error {
	return e.execute(run, w, runtime)
}

func (e *RunEngine) execute(run *workflow.WorkflowRun, w *workflow.Workflow, runtime interface{}) error {
	executionCount := 0

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

		durableSteps++
		run.State.Set(workflow.StateKeyDurableSteps, durableSteps)
		if durableSteps > workflow.MaxDurableExecutionsPerRun {
			log.Printf("[workflow] engine: run=%s DURABLE LOOP CIRCUIT BREAKER, %d lifetime node executions across waits, stopping",
				run.ID, durableSteps)
			run.SetError(workflow.ErrDurableExecutionsReached.Error())
			return e.runRepo.Update(run)
		}

		nodeVisitCounts[node.ID]++
		revisitLimit := maxNodeRevisits
		if node.Type == workflow.NodeTypeActionLoop || insideLoop[node.ID] {
			revisitLimit = workflow.MaxExecutionsPerRun
		} else if agentCycle[node.ID] {
			revisitLimit = maxAgentCycleRevisits
		}
		if nodeVisitCounts[node.ID] > revisitLimit {
			log.Printf("[workflow] engine: run=%s LOOP DETECTED, node=%s visited %d times, stopping",
				run.ID, node.ID, nodeVisitCounts[node.ID])
			run.SetError(fmt.Sprintf("loop detected: node %s visited %d times without waiting", node.ID, nodeVisitCounts[node.ID]))
			return e.runRepo.Update(run)
		}

		if node.Type.IsEnd() {
			log.Printf("[workflow] engine: run=%s reached end node=%s, completing", run.ID, node.ID)
			run.SetCompleted()
			e.writeLog(run.ID, node, workflow.LogStatusExecuted, nil, nil, "")
			return e.runRepo.Update(run)
		}

		executor, ok := e.registry.Get(node.Type)
		if !ok {

			if node.Type.IsTrigger() {
				log.Printf("[workflow] engine: run=%s trigger node=%s type=%s, passing through", run.ID, node.ID, node.Type)
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
				log.Printf("[workflow] engine: run=%s node=%s PANICKED, failing run: %v", run.ID, node.ID, err)
				run.SetError(err.Error())
				e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, nil, err.Error())
				return e.runRepo.Update(run)
			}
			if run.RetryCount < workflow.MaxRetriesPerNode {
				run.RetryCount++
				retryDelay := int64(run.RetryCount*run.RetryCount*5) * int64(time.Second)
				run.SetWaiting(time.Now().UTC().UnixMilli()+retryDelay/int64(time.Millisecond), workflow.WaitReasonRetry)
				log.Printf("[workflow] engine: run=%s node=%s RETRYING (%d/%d) in %ds, error: %v",
					run.ID, node.ID, run.RetryCount, workflow.MaxRetriesPerNode, run.RetryCount*run.RetryCount*5, err)
				e.writeLog(run.ID, node, workflow.LogStatusFailed, nil, nil, err.Error())
				return e.runRepo.Update(run)
			}
			log.Printf("[workflow] engine: run=%s node=%s FAILED (retries exhausted), error: %v",
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
