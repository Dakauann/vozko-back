package workflow_usecase

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	media_domain "vozko/domain/media"
	"vozko/domain/workflow"
)

const simCallerNumber = "5511999990000"

type keyData struct {
	Key string `json:"key"`
}

type waitingKeyPayload struct {
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type callTransferredPayload struct {
	QueueID   string `json:"queueId"`
	QueueName string `json:"queueName"`
	Notes     string `json:"notes,omitempty"`
}

func (s *wsWorkflowSimulation) runVoiceSimulation(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, wf *workflow.Workflow, trigger *workflow.Node, session SimulationSession) error {
	send := func(msgType string, payload interface{}) {
		_ = s.writeJSON(conn, writeMu, simServerMsg{Type: msgType, Payload: payload})
	}

	keys := make(chan rune, 8)
	ended := make(chan struct{})
	starts := make(chan simStart, 4)
	var hangUp sync.Once
	endCall := func() { hangUp.Do(func() { close(ended) }) }
	defer endCall()
	debugger := newSimDebugger(ctx, &wf.Graph, ended, s.deps.PauseIdleTimeout, func(p simPausedPayload) { send("paused", p) })
	go readSimulatedKeys(conn, keys, endCall, debugger, starts)
	go func() {
		select {
		case <-ctx.Done():
			endCall()
		case <-ended:
		}
	}()

	audio := newSimVoiceAudio(s.deps.MediaRepo, func(m *media_domain.Media) {
		send("message_sent", messagePayload{
			Direction: "outbound",
			Text:      m.DisplayName(),
			MsgType:   "audio",
			MessageID: "sim-audio-" + uuid.NewString(),
			AudioURL:  m.URL,
		})
	})
	registry := s.newSimulationRegistry(SimSandbox{
		WorkspaceID: wf.WorkspaceID, LeadID: uuid.NewString(), Messages: &simMessageRepo{}, Effects: newSimEffects(),
		RealCallsConfirmed: debugger.realCallsConfirmed,
	}, audio)
	runs := newSimRunRepo(func(*workflow.WorkflowRun) {})
	logs := newSimLogRepo(func(entry *workflow.WorkflowRunLog) {
		send("node_executed", debugger.nextStep(sandboxStep(entry, workflow.SandboxEffects{}, debugger.pinOf(entry.NodeID))))
	})

	call := newSimVoiceCall(keys, ended, func(timeout time.Duration) {
		send("waiting_key", waitingKeyPayload{TimeoutSeconds: int(timeout / time.Second)})
	}, func(transfer workflow.QueueTransfer) error {
		if s.deps.CallQueues == nil {
			return workflow.ErrNotTransferable
		}
		queue, err := s.deps.CallQueues.FindInWorkspace(ctx, wf.WorkspaceID, transfer.QueueID)
		if err != nil {
			return err
		}
		send("call_transferred", callTransferredPayload{QueueID: queue.ID, QueueName: queue.Name, Notes: transfer.Notes})
		return nil
	})
	run := newVoiceRun(wf, trigger, workflow.InboundVoiceCall{
		WorkspaceID:  wf.WorkspaceID,
		TrunkID:      wf.VoiceTrunkID(),
		CallID:       "sim-call-" + uuid.NewString(),
		CallerNumber: simCallerNumber,
		Call:         call,
	})
	if err := runs.Create(run); err != nil {
		return s.sendError(conn, writeMu, err.Error())
	}

	if session.AwaitStart {
		send("sim_ready", simReadyPayload{WorkflowID: wf.ID, TriggerType: string(trigger.Type), RealCallNodes: debugger.realCallNodes()})
		idle := debugger.idleTimer()
		defer idle.Stop()
		for started := false; !started; {
			select {
			case req := <-starts:
				if !req.reproduce {
					started = true
					continue
				}
				s.refuseCommand(conn, writeMu, "reproduce_run", errReproduceVoice)
			case <-ended:
				s.sendCancelled(conn, writeMu, simStopCancelled)
				return nil
			case <-idle.C:
				s.sendCancelled(conn, writeMu, simStopStartTimeout)
				return nil
			}
		}
	}

	send("sim_started", simStartedPayload{RunID: run.ID, WorkflowID: wf.ID, RealCallNodes: debugger.realCallNodes()})
	send("state_update", redactedState(run))
	engine := newSandboxEngine(runs, logs, registry, debugger)
	if err := engine.ExecuteWithRuntime(run, wf, call); err != nil {
		send("run_error", runErrorPayload(err.Error(), ""))
		return nil
	}
	send("state_update", redactedState(run))

	if reason := debugger.stopped(); reason != "" {
		s.sendCancelled(conn, writeMu, reason)
		return nil
	}
	switch run.Status {
	case workflow.RunStatusCompleted:
		send("run_completed", redactedState(run))
	case workflow.RunStatusCancelled:
		s.sendCancelled(conn, writeMu, simStopCancelled)
	default:
		send("run_error", runErrorPayload(run.Error, run.Status))
	}
	return nil
}

func readSimulatedKeys(conn *websocket.Conn, keys chan<- rune, endCall func(), debugger *simDebugger, starts chan<- simStart) {
	defer endCall()
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg simClientMsg
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "key":
			var d keyData
			if json.Unmarshal(msg.Data, &d) != nil || !workflow.IsDTMFKey(d.Key) {
				continue
			}
			select {
			case keys <- rune(d.Key[0]):
			default:
			}
		case "cancel":
			return
		default:
			if !debugger.handle(msg) {
				offerSimStart(msg, starts)
			}
		}
	}
}
