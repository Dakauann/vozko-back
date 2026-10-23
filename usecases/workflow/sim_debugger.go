package workflow_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"vozko/domain/workflow"
)

const defaultSimPauseIdleTimeout = 10 * time.Minute

const (
	simStopCancelled     = "cancelled"
	simStopPausedTimeout = "paused_timeout"
	simStopStartTimeout  = "start_timeout"
)

var (
	errSimCancelled     = errors.New("the simulation was cancelled while paused")
	errSimPausedTimeout = errors.New("the simulation stayed paused without a command for too long")
)

type simStepPayload struct {
	*workflow.WorkflowRunLog
	StepIndex int `json:"stepIndex"`
}

type simPausedPayload struct {
	NodeID    string                 `json:"nodeId"`
	NodeType  string                 `json:"nodeType"`
	StepIndex int                    `json:"stepIndex"`
	Reason    string                 `json:"reason"`
	State     map[string]interface{} `json:"state"`
}

type simNodeIDsData struct {
	NodeIDs []string `json:"nodeIds"`
}

type simNodeData struct {
	NodeID string `json:"nodeId"`
}

type simStart struct {
	reproduce     bool
	runID         string
	triggerNodeID string
	input         map[string]interface{}
	windowOpen    *bool
	malformed     bool
}

type simStartData struct {
	TriggerNodeID string                 `json:"triggerNodeId"`
	Input         map[string]interface{} `json:"input"`
	WindowOpen    *bool                  `json:"windowOpen"`
}

var errSimStartMalformed = errors.New("Os dados do start devem ser {\"triggerNodeId\": string, \"input\": objeto, \"windowOpen\": booleano}.")

type simReproduceData struct {
	RunID string `json:"runId"`
}

func startRequestOf(raw json.RawMessage) simStart {
	if len(raw) == 0 || string(raw) == "null" {
		return simStart{}
	}
	var data simStartData
	if err := json.Unmarshal(raw, &data); err != nil {
		return simStart{malformed: true}
	}
	return simStart{triggerNodeID: data.TriggerNodeID, input: data.Input, windowOpen: data.WindowOpen}
}

func offerSimStart(msg simClientMsg, starts chan<- simStart) {
	var start simStart
	switch msg.Type {
	case "start":
		start = startRequestOf(msg.Data)
	case "reproduce_run":
		var data simReproduceData
		_ = json.Unmarshal(msg.Data, &data)
		start = simStart{reproduce: true, runID: data.RunID}
	default:
		return
	}
	select {
	case starts <- start:
	default:
	}
}

type simDebugger struct {
	mu          sync.Mutex
	graph       *workflow.Graph
	breakpoints map[string]bool
	until       string
	stepping    bool
	paused      bool
	confirmed   bool
	pins        map[string]workflow.NodePin
	steps       int
	stopReason  string

	resume  chan struct{}
	cancel  <-chan struct{}
	done    <-chan struct{}
	idle    time.Duration
	onPause func(simPausedPayload)
}

func (d *simDebugger) idleTimer() *time.Timer {
	return time.NewTimer(d.idle)
}

func newSimDebugger(ctx context.Context, graph *workflow.Graph, cancel <-chan struct{}, idle time.Duration, onPause func(simPausedPayload)) *simDebugger {
	if idle <= 0 {
		idle = defaultSimPauseIdleTimeout
	}
	return &simDebugger{
		graph:       graph,
		breakpoints: map[string]bool{},
		pins:        map[string]workflow.NodePin{},
		resume:      make(chan struct{}, 1),
		cancel:      cancel,
		done:        ctx.Done(),
		idle:        idle,
		onPause:     onPause,
	}
}

func (d *simDebugger) handle(msg simClientMsg) bool {
	switch msg.Type {
	case "set_breakpoints":
		var data simNodeIDsData
		if json.Unmarshal(msg.Data, &data) == nil {
			d.setBreakpoints(data.NodeIDs)
		}
	case "run_until":
		var data simNodeData
		if json.Unmarshal(msg.Data, &data) == nil && data.NodeID != "" {
			d.command(func() { d.until, d.stepping = data.NodeID, false })
		}
	case "step":
		d.command(func() { d.stepping = true })
	case "continue":
		d.command(func() { d.stepping = false })
	case "confirm_real_calls":
		d.mu.Lock()
		d.confirmed = true
		d.mu.Unlock()
	default:
		return false
	}
	return true
}

func (d *simDebugger) setBreakpoints(ids []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.breakpoints = make(map[string]bool, len(ids))
	for _, id := range ids {
		d.breakpoints[id] = true
	}
}

func (d *simDebugger) command(apply func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	apply()
	if !d.paused {
		return
	}
	d.paused = false
	select {
	case d.resume <- struct{}{}:
	default:
	}
}

func (d *simDebugger) pauseAtStart() {
	d.mu.Lock()
	d.stepping = true
	d.mu.Unlock()
}

func (d *simDebugger) setPins(pins map[string]workflow.NodePin) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pins = pins
}

func (d *simDebugger) pinnedNodeIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := []string{}
	for _, n := range d.graph.Nodes {
		if _, ok := d.pins[n.ID]; ok {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

func (d *simDebugger) pinFor(node *workflow.Node) *workflow.NodePin {
	d.mu.Lock()
	defer d.mu.Unlock()
	if pin, ok := d.pins[node.ID]; ok {
		return &pin
	}
	return node.Pinned
}

func (d *simDebugger) pinOf(nodeID string) *workflow.NodePin {
	node := d.graph.FindNode(nodeID)
	if node == nil {
		return nil
	}
	return d.pinFor(node)
}

func (d *simDebugger) makesRealCall(node workflow.Node) bool {
	return workflow.ExternalCallNode(node) && !d.pinFor(&node).Servable()
}

func (d *simDebugger) realCallNodes() []string {
	ids := []string{}
	for _, n := range d.graph.Nodes {
		if d.makesRealCall(n) {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

func (d *simDebugger) nextStep(entry *workflow.WorkflowRunLog) simStepPayload {
	d.mu.Lock()
	index := d.steps
	d.steps++
	d.mu.Unlock()
	return simStepPayload{WorkflowRunLog: redactedStep(entry), StepIndex: index}
}

func (d *simDebugger) stopped() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopReason
}

func (d *simDebugger) stop(reason string, err error) error {
	d.mu.Lock()
	d.stopReason = reason
	d.paused = false
	d.mu.Unlock()
	return err
}

func (d *simDebugger) BeforeStep(run *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
	select {
	case <-d.cancel:
		return nil, d.stop(simStopCancelled, errSimCancelled)
	default:
	}
	if reason, pause := d.pauseReason(node.ID); pause {
		if err := d.waitWhilePaused(run, node, reason); err != nil {
			return nil, err
		}
	}
	return testStepGate(d.graph, node, d.pinFor(node), d.realCallsConfirmed(), &run.State), nil
}

func (d *simDebugger) realCallsConfirmed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.confirmed
}

func testStepGate(g *workflow.Graph, node *workflow.Node, pin *workflow.NodePin, realCallsConfirmed bool, state *workflow.RunState) *workflow.GatedStep {
	if pin.Servable() {
		return &workflow.GatedStep{Result: workflow.ServePinned(g, node, pin, state), Pinned: true}
	}
	if workflow.ExternalCallNode(*node) && !realCallsConfirmed {
		return &workflow.GatedStep{Result: &workflow.NodeResult{
			Error:     fmt.Sprintf("O nó %s faria uma chamada real neste teste. Confirme as chamadas reais ou fixe a saída do nó para testar sem chamar.", node.ID),
			ErrorKind: workflow.ErrorKindRealCallNotConfirmed,
		}}
	}
	return nil
}

func (d *simDebugger) pauseReason(nodeID string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	reason := ""
	switch {
	case d.until == nodeID:
		reason, d.until = "run_until", ""
	case d.breakpoints[nodeID]:
		reason = "breakpoint"
	case d.stepping:
		reason = "step"
	default:
		return "", false
	}
	d.stepping = false
	d.paused = true
	select {
	case <-d.resume:
	default:
	}
	return reason, true
}

func (d *simDebugger) waitWhilePaused(run *workflow.WorkflowRun, node *workflow.Node, reason string) error {
	d.mu.Lock()
	index := d.steps
	d.mu.Unlock()
	d.onPause(simPausedPayload{
		NodeID: node.ID, NodeType: string(node.Type), StepIndex: index, Reason: reason,
		State: redactedState(run).Vars,
	})
	idle := time.NewTimer(d.idle)
	defer idle.Stop()
	select {
	case <-d.resume:
		return nil
	case <-d.cancel:
		return d.stop(simStopCancelled, errSimCancelled)
	case <-d.done:
		return d.stop(simStopCancelled, context.Canceled)
	case <-idle.C:
		return d.stop(simStopPausedTimeout, errSimPausedTimeout)
	}
}
