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

func (s *wsWorkflowSimulation) runVoiceSimulation(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, wf *workflow.Workflow, trigger *workflow.Node) error {
	send := func(msgType string, payload interface{}) {
		_ = s.writeJSON(conn, writeMu, simServerMsg{Type: msgType, Payload: payload})
	}

	keys := make(chan rune, 8)
	ended := make(chan struct{})
	var hangUp sync.Once
	endCall := func() { hangUp.Do(func() { close(ended) }) }
	defer endCall()
	go readSimulatedKeys(conn, keys, endCall)
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
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{VoiceAudio: audio})
	runs := newSimRunRepo(func(*workflow.WorkflowRun) {})
	logs := newSimLogRepo(func(entry *workflow.WorkflowRunLog) {
		send("node_executed", nodeEventPayload{
			NodeID:   entry.NodeID,
			NodeType: string(entry.NodeType),
			Output:   entry.Output,
			Error:    entry.Error,
		})
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

	send("sim_started", map[string]string{"runId": run.ID, "workflowId": wf.ID})
	send("state_update", statePayload{Vars: run.State.Vars})
	if err := NewRunEngine(runs, logs, registry).ExecuteWithRuntime(run, wf, call); err != nil {
		send("run_error", map[string]string{"error": err.Error()})
		return nil
	}
	send("state_update", statePayload{Vars: run.State.Vars})

	switch run.Status {
	case workflow.RunStatusCompleted:
		send("run_completed", statePayload{Vars: run.State.Vars})
	case workflow.RunStatusCancelled:
		send("run_cancelled", nil)
	default:
		send("run_error", map[string]string{"error": run.Error, "status": string(run.Status)})
	}
	return nil
}

func readSimulatedKeys(conn *websocket.Conn, keys chan<- rune, endCall func()) {
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
		}
	}
}
