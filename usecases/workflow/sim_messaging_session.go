package workflow_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"vozko/domain/workflow"
)

type messagingSim struct {
	s       *wsWorkflowSimulation
	ctx     context.Context
	conn    *websocket.Conn
	writeMu *sync.Mutex
	session SimulationSession
	wf      *workflow.Workflow
	trigger *workflow.Node
	run     *workflow.WorkflowRun
	entryID string
	leadID  string

	specs []workflow.TriggerInputSpec
	spec  workflow.TriggerInputSpec

	window  *simMessageWindowRepo
	inbound chan simInbound
	forms   chan simFormReply
	cancel  chan struct{}
	vars    chan setVariableData
	starts  chan simStart
	done    chan *workflow.WorkflowRun

	currentMu sync.Mutex
	current   string

	debugger *simDebugger
	runs     *simRunRepo
	messages *simMessageRepo
	registry *NodeExecutorRegistry
	engine   *RunEngine
}

func (s *wsWorkflowSimulation) loadSimWorkflow(session SimulationSession) (*workflow.Workflow, *workflow.Node, error) {
	wf, err := s.deps.WorkflowRepo.FindByID(session.WorkflowID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load workflow: %v", err)
	}
	if wf == nil {
		return nil, nil, errors.New("workflow not found")
	}
	if wf.WorkspaceID != session.WorkspaceID {
		return nil, nil, errors.New("workspace mismatch: workflow belongs to a different workspace")
	}
	wf = withheldFor(s.deps.PinSources, session.Viewer, wf)
	wf.Normalize()
	trigger := wf.Graph.TriggerNode()
	if trigger == nil {
		return nil, nil, errors.New("workflow has no trigger node")
	}
	return wf, trigger, nil
}

func (s *wsWorkflowSimulation) newMessagingSim(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, session SimulationSession, wf *workflow.Workflow, trigger *workflow.Node) *messagingSim {
	m := &messagingSim{
		s: s, ctx: ctx, conn: conn, writeMu: writeMu, session: session, wf: wf, trigger: trigger,
		entryID:  "sim-entry-" + uuid.New().String(),
		leadID:   "sim-lead-" + uuid.New().String(),
		specs:    s.deps.TriggerInputs.Specs(wf.WorkspaceID, &wf.Graph),
		inbound:  make(chan simInbound, 1),
		forms:    make(chan simFormReply, 1),
		cancel:   make(chan struct{}, 1),
		vars:     make(chan setVariableData, 10),
		starts:   make(chan simStart, 4),
		done:     make(chan *workflow.WorkflowRun, 1),
		messages: &simMessageRepo{},
	}
	m.window = newSimMessageWindow(m.leadID, false)
	m.debugger = newSimDebugger(ctx, &wf.Graph, m.cancel, s.deps.PauseIdleTimeout, func(p simPausedPayload) {
		m.write("paused", p)
	})
	effects := newSimEffects()
	m.runs = newSimRunRepo(m.onRunUpdate)
	logs := newSimLogRepo(func(logEntry *workflow.WorkflowRunLog) {
		step := storedStep(sandboxStep(logEntry, effects.drain(), m.debugger.pinOf(logEntry.NodeID)))
		m.write("node_executed", m.debugger.nextStep(step))
	})
	m.registry = s.newSimulationRegistry(SimSandbox{
		WorkspaceID: wf.WorkspaceID, LeadID: m.leadID, Messages: m.messages, Outbound: m.deliverOutbound, Window: m.window, Effects: effects,
		RealCallsConfirmed: m.debugger.realCallsConfirmed,
	}, nil)
	m.engine = newSandboxEngine(m.runs, logs, m.registry, steppingGate{gate: m.debugger, stepping: m.setCurrent})
	now := time.Now().UTC()
	m.run = &workflow.WorkflowRun{
		ID:            uuid.New().String(),
		WorkflowID:    wf.ID,
		WorkspaceID:   wf.WorkspaceID,
		EntryID:       m.entryID,
		EntryType:     "whatsapp",
		Status:        workflow.RunStatusRunning,
		CurrentNodeID: trigger.ID,
		State:         workflow.NewRunState(),
		StartedAt:     now,
		UpdatedAt:     now,
	}
	m.spec, _ = workflow.FindTriggerInputSpec(m.specs, trigger.ID)
	m.useTrigger(trigger, m.spec)
	return m
}

func (m *messagingSim) useTrigger(node *workflow.Node, spec workflow.TriggerInputSpec) {
	m.trigger, m.spec = node, spec
	m.run.CurrentNodeID = node.ID
	m.run.TriggerNodeID = node.ID
	m.run.TriggerType = workflow.TriggerType(node.Type)
	m.window.startAs(spec.WindowOpen(nil))
}

func (m *messagingSim) subject() workflow.TriggerTestSubject {
	return workflow.TriggerTestSubject{
		RunID: m.run.ID, EntryID: m.entryID, ContactNumber: simContactNumber,
		LeadID: m.leadID, LeadName: simLeadName, Today: time.Now().UTC(),
	}
}

func (m *messagingSim) chooseTrigger(req simStart) error {
	if req.malformed {
		return errSimStartMalformed
	}
	nodeID := req.triggerNodeID
	if nodeID == "" {
		nodeID = m.trigger.ID
	}
	spec, data, err := startFromTrigger(m.specs, nodeID, req.input, m.subject())
	if err != nil {
		slog.Info("workflow.simulation.start_refused",
			slog.String("run_id", m.run.ID), slog.String("node_id", nodeID), slog.String("workspace_id", m.wf.WorkspaceID), slog.String("error", err.Error()))
		return err
	}
	m.useTrigger(m.wf.Graph.FindNode(spec.NodeID), spec)
	if req.windowOpen != nil {
		m.window.startAs(spec.WindowOpen(req.windowOpen))
	}
	for k, v := range data {
		m.run.State.Set(k, v)
	}
	slog.Info("workflow.simulation.trigger_chosen",
		slog.String("run_id", m.run.ID), slog.String("node_id", spec.NodeID), slog.String("workspace_id", m.wf.WorkspaceID))
	return nil
}

func (m *messagingSim) write(msgType string, payload interface{}) {
	_ = m.s.writeJSON(m.conn, m.writeMu, simServerMsg{Type: msgType, Payload: payload})
}

func (m *messagingSim) onRunUpdate(run *workflow.WorkflowRun) {
	if !run.Status.IsTerminal() {
		return
	}
	select {
	case m.done <- run:
	default:
	}
}

func (m *messagingSim) play() error {
	go m.readClientMessages()
	log.Printf("[workflow-sim] starting simulation for workflow=%s run=%s", m.wf.ID, m.run.ID)

	reproducing, stop, err := m.awaitStart()
	if stop {
		return err
	}
	if m.needsInitialMessage() && reproducing == nil {
		if reproducing, stop, err = m.awaitTrigger(); stop {
			return err
		}
	}
	m.applyReproduction(reproducing)
	m.run.TriggerData = workflow.TriggerDataOf(m.run.State)
	m.write("sim_started", simStartedPayload{
		RunID:         m.run.ID,
		WorkflowID:    m.wf.ID,
		RealCallNodes: m.debugger.realCallNodes(),
		Reproducing:   reproducing,
		Trigger:       simTriggerOf(m.trigger),
	})
	return m.runUntilDone()
}

func (m *messagingSim) signalCancel() {
	select {
	case m.cancel <- struct{}{}:
	default:
	}
}

func (m *messagingSim) readClientMessages() {
	defer m.signalCancel()
	for {
		_, raw, err := m.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg simClientMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if m.routeClientMessage(msg) {
			return
		}
	}
}

func (m *messagingSim) routeClientMessage(msg simClientMsg) bool {
	if isSimReply(msg.Type) {
		in, err := parseSimInbound(msg)
		if err != nil {
			m.s.refuseCommand(m.conn, m.writeMu, msg.Type, err)
			return false
		}
		select {
		case m.inbound <- in:
		default:
		}
		return false
	}
	switch msg.Type {
	case "form_reply":
		var d simFormReply
		if json.Unmarshal(msg.Data, &d) == nil {
			select {
			case m.forms <- d:
			default:
			}
		}
	case "cancel":
		m.signalCancel()
		return true
	case "set_variable":
		var d setVariableData
		if json.Unmarshal(msg.Data, &d) == nil && d.Key != "" {
			select {
			case m.vars <- d:
			default:
			}
		}
	default:
		if !m.debugger.handle(msg) {
			offerSimStart(msg, m.starts)
		}
	}
	return false
}

func (m *messagingSim) setCurrent(nodeID string) {
	m.currentMu.Lock()
	m.current = nodeID
	m.currentMu.Unlock()
}

func (m *messagingSim) currentNode() string {
	m.currentMu.Lock()
	defer m.currentMu.Unlock()
	return m.current
}

func (m *messagingSim) deliverOutbound(msg SimOutboundMessage) {
	nodeID := m.currentNode()
	if msg.Message != nil {
		stored := *msg.Message
		_ = m.messages.Create(&stored)
	}
	m.write("message_sent", messagePayload{
		Direction:   "outbound",
		Text:        msg.Text,
		MsgType:     msg.Type,
		NodeID:      nodeID,
		NodeName:    workflow.NodeNameOf(m.wf.Graph.FindNode(nodeID), m.registry),
		MessageID:   msg.MessageID,
		AudioBase64: msg.AudioBase64,
		AudioMime:   msg.AudioMime,
		Message:     msg.Message,
	})
}

type steppingGate struct {
	gate     workflow.StepGate
	stepping func(nodeID string)
}

func (g steppingGate) BeforeStep(run *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
	g.stepping(node.ID)
	return g.gate.BeforeStep(run, node)
}

func (m *messagingSim) triggerType() workflow.TriggerType {
	return workflow.TriggerType(m.trigger.Type)
}

func (m *messagingSim) needsInitialMessage() bool {
	return m.spec.Shape == workflow.TriggerInputMessage
}

func (m *messagingSim) cancelled(reason string) error {
	m.run.Status = workflow.RunStatusCancelled
	_ = m.runs.Update(m.run)
	m.s.sendCancelled(m.conn, m.writeMu, reason)
	return nil
}

func (m *messagingSim) receive(in simInbound) {
	m.window.customerWrote()
	message := in.message(m.entryID)
	_ = m.messages.Create(&message)
	m.write("message_received", receivedPayload{Direction: "inbound", Message: message})
}

func (m *messagingSim) tryReproduce(req simStart) *simReproduction {
	r, err := m.s.prepareReproduction(m.session, m.wf, req.runID, workflow.TestBehaviorsOf(m.registry.Catalog()))
	if err != nil {
		m.s.refuseCommand(m.conn, m.writeMu, "reproduce_run", err)
		return nil
	}
	return r
}

func (m *messagingSim) awaitStart() (*simReproduction, bool, error) {
	if m.session.AwaitStart {
		m.write("sim_ready", simReadyPayload{
			WorkflowID: m.wf.ID, TriggerType: string(m.triggerType()), RealCallNodes: m.debugger.realCallNodes(),
			Triggers: m.specs, DefaultTriggerNodeID: m.trigger.ID,
		})
		return m.waitForStart()
	}
	if m.needsInitialMessage() {
		return nil, false, nil
	}
	err := m.chooseTrigger(simStart{})
	if err == nil {
		return nil, false, nil
	}
	m.s.refuseCommand(m.conn, m.writeMu, "start", err)
	return m.waitForStart()
}

func (m *messagingSim) waitForStart() (*simReproduction, bool, error) {
	idle := m.debugger.idleTimer()
	defer idle.Stop()
	for {
		select {
		case req := <-m.starts:
			if !req.reproduce {
				if err := m.chooseTrigger(req); err != nil {
					m.s.refuseCommand(m.conn, m.writeMu, "start", err)
					continue
				}
				return nil, false, nil
			}
			if reproducing := m.tryReproduce(req); reproducing != nil {
				return reproducing, false, nil
			}
		case <-m.cancel:
			return nil, true, m.cancelled(simStopCancelled)
		case <-idle.C:
			return nil, true, m.cancelled(simStopStartTimeout)
		case <-m.ctx.Done():
			return nil, true, m.ctx.Err()
		}
	}
}

func (m *messagingSim) awaitTrigger() (*simReproduction, bool, error) {
	m.write("waiting_trigger", map[string]string{"triggerType": string(m.triggerType()), "nodeId": m.trigger.ID})
	for {
		select {
		case initial := <-m.inbound:
			for k, v := range initial.data {
				m.run.State.Set(k, v)
			}
			m.run.State.Set(workflow.DataKeyContactNumber, simContactNumber)
			m.receive(initial)
			return nil, false, nil
		case req := <-m.starts:
			if !req.reproduce {
				continue
			}
			if reproducing := m.tryReproduce(req); reproducing != nil {
				return reproducing, false, nil
			}
		case <-m.cancel:
			return nil, true, m.cancelled(simStopCancelled)
		case <-m.ctx.Done():
			return nil, true, m.ctx.Err()
		}
	}
}

func (m *messagingSim) applyReproduction(reproducing *simReproduction) {
	if reproducing == nil {
		return
	}
	if node := m.wf.Graph.FindNode(reproducing.triggerNodeID); node != nil && node.Type.IsTrigger() {
		spec, _ := workflow.FindTriggerInputSpec(m.specs, node.ID)
		m.useTrigger(node, spec)
	}
	for k, v := range reproducing.seed {
		m.run.State.Set(k, v)
	}
	if message, _ := reproducing.seed["message"].(string); message != "" {
		m.receive(textInbound(message))
	}
	m.debugger.setPins(reproducing.pins)
	m.debugger.pauseAtStart()
	reproducing.PinnedNodes = m.debugger.pinnedNodeIDs()
}

func (m *messagingSim) drainVariables() {
	for {
		select {
		case v := <-m.vars:
			m.run.State.Set(v.Key, v.Value)
		default:
			return
		}
	}
}

func (m *messagingSim) reportIfDone() bool {
	select {
	case termRun := <-m.done:
		if termRun.Status == workflow.RunStatusCompleted {
			m.write("run_completed", redactedState(termRun))
		} else {
			m.write("run_error", runErrorPayload(termRun.Error, termRun.Status))
		}
		return true
	default:
		return false
	}
}

func (m *messagingSim) runUntilDone() error {
	for {
		m.drainVariables()
		m.setCurrent(m.run.CurrentNodeID)
		m.write("state_update", redactedState(m.run))

		if engineErr := m.engine.Execute(m.run, m.wf); engineErr != nil {
			m.write("run_error", runErrorPayload(engineErr.Error(), ""))
			return nil
		}
		if reason := m.debugger.stopped(); reason != "" {
			m.s.sendCancelled(m.conn, m.writeMu, reason)
			return nil
		}
		m.write("state_update", redactedState(m.run))

		if m.reportIfDone() {
			return nil
		}
		if m.run.Status == workflow.RunStatusWaiting {
			if stop, err := m.resumeWait(); stop {
				return err
			}
			continue
		}
		if !m.reportIfDone() {
			m.write("run_completed", redactedState(m.run))
		}
		return nil
	}
}

func (m *messagingSim) resumeWait() (bool, error) {
	waitNode := m.wf.Graph.FindNode(m.run.CurrentNodeID)
	if waitNode == nil {
		_ = m.s.sendError(m.conn, m.writeMu, "waiting on unknown node")
		return true, nil
	}
	switch m.run.WaitReason {
	case workflow.WaitReasonForm:
		return m.awaitForm(waitNode)
	case workflow.WaitReasonReply:
		return m.awaitReply(waitNode)
	case workflow.WaitReasonDuration:
		return m.skipWait(waitNode, "duration", "completed", "")
	case workflow.WaitReasonEvent:
		return m.skipWait(waitNode, "event", "event", "event")
	case workflow.WaitReasonRetry:
		m.run.SetRunning()
		m.run.UpdatedAt = time.Now().UTC()
		_ = m.runs.Update(m.run)
		return false, nil
	default:
		_ = m.s.sendError(m.conn, m.writeMu, fmt.Sprintf("unexpected wait reason: %s", m.run.WaitReason))
		return true, nil
	}
}

func (m *messagingSim) followOrComplete(nextID string) bool {
	if nextID == "" {
		m.run.SetCompleted()
		_ = m.runs.Update(m.run)
		m.write("run_completed", redactedState(m.run))
		return true
	}
	m.run.SetRunning()
	m.run.State.Set("_prev_node_id", m.run.CurrentNodeID)
	m.run.CurrentNodeID = nextID
	m.run.UpdatedAt = time.Now().UTC()
	_ = m.runs.Update(m.run)
	return false
}

func (m *messagingSim) skipWait(waitNode *workflow.Node, reason, outcome, edgeLabel string) (bool, error) {
	m.write("wait_skipped", map[string]string{"nodeId": waitNode.ID, "reason": reason})
	edges := m.wf.Graph.OutgoingEdges(m.run.CurrentNodeID)
	nextID := ""
	if edgeLabel != "" {
		nextID = findExactEdgeByLabel(edges, edgeLabel)
	}
	if nextID == "" && len(edges) > 0 {
		nextID = edges[0].Target
	}
	if nextID != "" {
		m.run.State.Set("_wait_outcome", outcome)
	}
	return m.followOrComplete(nextID), nil
}

func (m *messagingSim) awaitForm(waitNode *workflow.Node) (bool, error) {
	timeoutSec := simTimeoutSeconds(waitNode)
	formID, _ := waitNode.Config[workflow.FormNodeFormIDKey].(string)
	m.write("waiting_form", waitingFormPayload{NodeID: waitNode.ID, FormID: formID, TimeoutSeconds: timeoutSec})

	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()
	for {
		select {
		case answer := <-m.forms:
			if !answer.answered() {
				return m.followOrComplete(unansweredEdge(m.run, m.wf, waitNode)), nil
			}
			m.window.customerWrote()
			if err := AdvanceOnForm(m.run, m.wf, answer.runData()); err != nil {
				_ = m.s.sendError(m.conn, m.writeMu, err.Error())
				return true, nil
			}
			_ = m.runs.Update(m.run)
			return false, nil
		case in := <-m.inbound:
			m.receive(in)
		case <-timer.C:
			return m.followOrComplete(unansweredEdge(m.run, m.wf, waitNode)), nil
		case <-m.cancel:
			return true, m.cancelled(simStopCancelled)
		case <-m.ctx.Done():
			return true, m.ctx.Err()
		}
	}
}

func (m *messagingSim) awaitReply(waitNode *workflow.Node) (bool, error) {
	timeoutSec := simTimeoutSeconds(waitNode)
	m.write("waiting_reply", waitingReplyPayload{NodeID: waitNode.ID, TimeoutSeconds: timeoutSec})

	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()
	for {
		select {
		case in := <-m.inbound:
			m.receive(in)
			err := AdvanceOnReply(m.run, m.wf, in.replyData(waitNode))
			if errors.Is(err, ErrInteractiveReplyUnhandled) {
				m.write("reply_ignored", replyIgnoredPayload{NodeID: waitNode.ID, Reason: "interactive_reply_unhandled"})
				continue
			}
			if err != nil {
				_ = m.s.sendError(m.conn, m.writeMu, err.Error())
				return true, nil
			}
			_ = m.runs.Update(m.run)
			return false, nil
		case <-timer.C:
			return m.followOrComplete(unansweredEdge(m.run, m.wf, waitNode)), nil
		case <-m.cancel:
			return true, m.cancelled(simStopCancelled)
		case <-m.ctx.Done():
			return true, m.ctx.Err()
		}
	}
}
