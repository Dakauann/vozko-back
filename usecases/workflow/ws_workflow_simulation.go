package workflow_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/cache"
	"vozko/domain/calendar"
	"vozko/domain/conversation"
	media_domain "vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	"vozko/domain/tools"
	template_domain "vozko/domain/whatsapp/template"
	"vozko/domain/workflow"
	calendar_usecase "vozko/usecases/calendar"
)

type simServerMsg struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type nodeEventPayload struct {
	NodeID   string                 `json:"nodeId"`
	NodeType string                 `json:"nodeType"`
	Output   map[string]interface{} `json:"output,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

type messagePayload struct {
	Direction   string `json:"direction"`
	Text        string `json:"text"`
	MsgType     string `json:"msgType"`
	NodeID      string `json:"nodeId,omitempty"`
	MessageID   string `json:"messageId"`
	AudioBase64 string `json:"audioBase64,omitempty"`
	AudioMime   string `json:"audioMime,omitempty"`
}

type waitingReplyPayload struct {
	NodeID         string `json:"nodeId"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type statePayload struct {
	Vars map[string]interface{} `json:"vars"`
}

type simClientMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type replyData struct {
	Text string `json:"text"`
}

type setVariableData struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

type WSWorkflowSimulationDeps struct {
	WorkflowRepo   workflow.WorkflowRepository
	TemplateRepo   template_domain.Repository
	MediaRepo      media_domain.MediaRepository
	AIService      ai.Service
	AgentRepo      agent.Repository
	ToolRegistry   tools.Service
	SharedState    cache.SharedState
	CalendarRepo   calendar.Repository
	GoogleCalendar calendar.GoogleOAuthService
	BillingPub     messaging.MessageQueuePub
}

type WSWorkflowSimulationUseCase interface {
	HandleSession(ctx context.Context, conn *websocket.Conn, workflowID, workspaceID string) error
}

type wsWorkflowSimulation struct {
	deps WSWorkflowSimulationDeps
}

func NewWSWorkflowSimulationUseCase(deps WSWorkflowSimulationDeps) WSWorkflowSimulationUseCase {
	return &wsWorkflowSimulation{deps: deps}
}

func (s *wsWorkflowSimulation) HandleSession(ctx context.Context, conn *websocket.Conn, workflowID, workspaceID string) error {

	var writeMu sync.Mutex

	wf, err := s.deps.WorkflowRepo.FindByID(workflowID)
	if err != nil {
		return s.sendError(conn, &writeMu, fmt.Sprintf("failed to load workflow: %v", err))
	}
	if wf == nil {
		return s.sendError(conn, &writeMu, "workflow not found")
	}

	if wf.WorkspaceID != workspaceID {
		return s.sendError(conn, &writeMu, "workspace mismatch: workflow belongs to a different workspace")
	}

	workspaceID = wf.WorkspaceID

	wf.Normalize()

	trigger := wf.Graph.TriggerNode()
	if trigger == nil {
		return s.sendError(conn, &writeMu, "workflow has no trigger node")
	}
	outboundCh := make(chan SimOutboundMessage, 100)
	defer close(outboundCh)

	simLeadID := "sim-lead-" + uuid.New().String()
	simEntryID := "sim-entry-" + uuid.New().String()

	replyCh := make(chan string, 1)
	cancelCh := make(chan struct{}, 1)
	varCh := make(chan setVariableData, 10)

	var currentNodeID string
	var currentNodeMu sync.Mutex

	waitingCh := make(chan *workflow.WorkflowRun, 1)
	doneCh := make(chan *workflow.WorkflowRun, 1)

	onRunUpdate := func(run *workflow.WorkflowRun) {
		switch run.Status {
		case workflow.RunStatusWaiting:
			select {
			case waitingCh <- run:
			default:
			}
		case workflow.RunStatusCompleted, workflow.RunStatusError, workflow.RunStatusCancelled:
			select {
			case doneCh <- run:
			default:
			}
		}
	}

	onLogWrite := func(logEntry *workflow.WorkflowRunLog) {
		_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "node_executed", Payload: nodeEventPayload{
			NodeID:   logEntry.NodeID,
			NodeType: string(logEntry.NodeType),
			Output:   logEntry.Output,
			Error:    logEntry.Error,
		}})
	}

	simRunRepo := newSimRunRepo(onRunUpdate)
	simLogRepo := newSimLogRepo(onLogWrite)
	simMsgRepo := &simMessageRepo{}

	registry := s.newSimulationRegistry(workspaceID, simLeadID, simMsgRepo, outboundCh)

	engine := NewRunEngine(simRunRepo, simLogRepo, registry)

	now := time.Now().UTC()
	run := &workflow.WorkflowRun{
		ID:            uuid.New().String(),
		WorkflowID:    wf.ID,
		WorkspaceID:   workspaceID,
		EntryID:       simEntryID,
		EntryType:     "whatsapp",
		Status:        workflow.RunStatusRunning,
		CurrentNodeID: trigger.ID,
		State:         workflow.NewRunState(),
		StartedAt:     now,
		UpdatedAt:     now,
	}

	if err := simRunRepo.Create(run); err != nil {
		return s.sendError(conn, &writeMu, fmt.Sprintf("failed to create sim run: %v", err))
	}

	go func() {
		defer func() {
			select {
			case cancelCh <- struct{}{}:
			default:
			}
		}()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg simClientMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "reply":
				var d replyData
				if json.Unmarshal(msg.Data, &d) == nil && d.Text != "" {
					select {
					case replyCh <- d.Text:
					default:
					}
				}
			case "cancel":
				select {
				case cancelCh <- struct{}{}:
				default:
				}
				return
			case "set_variable":
				var d setVariableData
				if json.Unmarshal(msg.Data, &d) == nil && d.Key != "" {
					select {
					case varCh <- d:
					default:
					}
				}
			}
		}
	}()

	go func() {
		for msg := range outboundCh {
			currentNodeMu.Lock()
			nodeID := currentNodeID
			currentNodeMu.Unlock()

			_ = simMsgRepo.Create(&conversation.Message{
				ID:          msg.MessageID,
				EntryID:     simEntryID,
				EntryType:   shared.EntryType("whatsapp"),
				MessageType: conversation.MessageTypeAIResponse,
				From:        "sim-business-phone",
				To:          "5511999990000",
				Text:        msg.Text,
				CreatedAt:   time.Now().UTC(),
			})

			_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "message_sent", Payload: messagePayload{
				Direction:   "outbound",
				Text:        msg.Text,
				MsgType:     msg.Type,
				NodeID:      nodeID,
				MessageID:   msg.MessageID,
				AudioBase64: msg.AudioBase64,
				AudioMime:   msg.AudioMime,
			}})
		}
	}()

	log.Printf("[workflow-sim] starting simulation for workflow=%s run=%s", wf.ID, run.ID)

	simTriggerType := workflow.TriggerType(trigger.Type)
	needsInitialMessage := wf.HasTriggerType(workflow.TriggerMessageReceived) ||
		wf.HasTriggerType(workflow.TriggerFirstMessage)

	if needsInitialMessage {
		_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "waiting_trigger", Payload: map[string]string{
			"triggerType": string(simTriggerType),
		}})

		select {
		case initialMsg := <-replyCh:

			run.State.Set("message", initialMsg)

			_ = simMsgRepo.Create(&conversation.Message{
				ID:          "sim-msg-" + uuid.New().String(),
				EntryID:     simEntryID,
				EntryType:   shared.EntryType("whatsapp"),
				MessageType: conversation.MessageTypeUserMessage,
				From:        "5511999990000",
				To:          "sim-business-phone",
				Text:        initialMsg,
				CreatedAt:   time.Now().UTC(),
			})

		case <-cancelCh:
			run.Status = workflow.RunStatusCancelled
			_ = simRunRepo.Update(run)
			_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_cancelled"})
			return nil

		case <-ctx.Done():
			return ctx.Err()
		}
	}

	_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "sim_started", Payload: map[string]string{
		"runId":      run.ID,
		"workflowId": wf.ID,
	}})

	for {

	drainVars:
		for {
			select {
			case v := <-varCh:
				run.State.Set(v.Key, v.Value)
			default:
				break drainVars
			}
		}

		currentNodeMu.Lock()
		currentNodeID = run.CurrentNodeID
		currentNodeMu.Unlock()

		_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "state_update", Payload: statePayload{Vars: run.State.Vars}})

		engineErr := engine.Execute(run, wf)
		if engineErr != nil {
			_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_error", Payload: map[string]string{
				"error": engineErr.Error(),
			}})
			return nil
		}

		_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "state_update", Payload: statePayload{Vars: run.State.Vars}})

		select {
		case termRun := <-doneCh:
			if termRun.Status == workflow.RunStatusCompleted {
				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_completed", Payload: statePayload{Vars: termRun.State.Vars}})
			} else {
				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_error", Payload: map[string]string{
					"error":  termRun.Error,
					"status": string(termRun.Status),
				}})
			}
			return nil
		default:
		}

		if run.Status == workflow.RunStatusWaiting {
			waitNode := wf.Graph.FindNode(run.CurrentNodeID)
			if waitNode == nil {
				_ = s.sendError(conn, &writeMu, "waiting on unknown node")
				return nil
			}

			switch run.WaitReason {
			case workflow.WaitReasonReply:
				timeoutSec := 300
				if ts, ok := waitNode.Config["timeout_seconds"].(float64); ok && ts > 0 {
					timeoutSec = int(ts)
				}

				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "waiting_reply", Payload: waitingReplyPayload{
					NodeID:         waitNode.ID,
					TimeoutSeconds: timeoutSec,
				}})

				timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
				select {
				case replyText := <-replyCh:
					timer.Stop()

					// Same reply-resume path production uses (AdvanceOnReply) so the
					// simulator can't diverge from real behavior. For an interactive
					// prompt (buttons/list) the typed reply IS the chosen option id,
					// mirroring the button/list reply id production receives.
					replyData := map[string]interface{}{"message": replyText}
					if waitNode.Type.IsInteractivePrompt() {
						replyData["selected_option_id"] = replyText
						replyData["selected_option_title"] = replyText
						replyData["selected_option_type"] = "button"
					}
					if err := AdvanceOnReply(run, wf, replyData); err != nil {
						_ = s.sendError(conn, &writeMu, err.Error())
						return nil
					}
					_ = simMsgRepo.Create(&conversation.Message{
						ID:          "sim-msg-" + uuid.New().String(),
						EntryID:     simEntryID,
						EntryType:   shared.EntryType("whatsapp"),
						MessageType: conversation.MessageTypeUserMessage,
						From:        "5511999990000",
						To:          "sim-business-phone",
						Text:        replyText,
						CreatedAt:   time.Now().UTC(),
					})
					_ = simRunRepo.Update(run)

				case <-timer.C:

					edges := wf.Graph.OutgoingEdges(run.CurrentNodeID)
					nextID := findExactEdgeByLabel(edges, "timeout")
					if nextID == "" {

						run.SetCompleted()
						_ = simRunRepo.Update(run)
						_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_completed", Payload: statePayload{Vars: run.State.Vars}})
						return nil
					}
					run.State.Set("_wait_outcome", "timeout")
					run.SetRunning()
					run.State.Set("_prev_node_id", run.CurrentNodeID)
					run.CurrentNodeID = nextID
					run.UpdatedAt = time.Now().UTC()
					_ = simRunRepo.Update(run)

				case <-cancelCh:
					run.Status = workflow.RunStatusCancelled
					_ = simRunRepo.Update(run)
					_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_cancelled"})
					return nil

				case <-ctx.Done():
					return ctx.Err()
				}

			case workflow.WaitReasonDuration:

				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "wait_skipped", Payload: map[string]string{
					"nodeId": waitNode.ID,
					"reason": "duration",
				}})
				edges := wf.Graph.OutgoingEdges(run.CurrentNodeID)
				if len(edges) == 0 {
					run.SetCompleted()
					_ = simRunRepo.Update(run)
					_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_completed", Payload: statePayload{Vars: run.State.Vars}})
					return nil
				}

				nextID := edges[0].Target
				run.State.Set("_wait_outcome", "completed")
				run.SetRunning()
				run.State.Set("_prev_node_id", run.CurrentNodeID)
				run.CurrentNodeID = nextID
				run.UpdatedAt = time.Now().UTC()
				_ = simRunRepo.Update(run)

			case workflow.WaitReasonEvent:

				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "wait_skipped", Payload: map[string]string{
					"nodeId": waitNode.ID,
					"reason": "event",
				}})
				edges := wf.Graph.OutgoingEdges(run.CurrentNodeID)
				nextID := findExactEdgeByLabel(edges, "event")
				if nextID == "" && len(edges) > 0 {
					nextID = edges[0].Target
				}
				if nextID == "" {
					run.SetCompleted()
					_ = simRunRepo.Update(run)
					_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_completed", Payload: statePayload{Vars: run.State.Vars}})
					return nil
				}
				run.State.Set("_wait_outcome", "event")
				run.SetRunning()
				run.State.Set("_prev_node_id", run.CurrentNodeID)
				run.CurrentNodeID = nextID
				run.UpdatedAt = time.Now().UTC()
				_ = simRunRepo.Update(run)

			case workflow.WaitReasonRetry:

				run.SetRunning()
				run.UpdatedAt = time.Now().UTC()
				_ = simRunRepo.Update(run)

			default:
				_ = s.sendError(conn, &writeMu, fmt.Sprintf("unexpected wait reason: %s", run.WaitReason))
				return nil
			}

			continue
		}

		select {
		case termRun := <-doneCh:
			if termRun.Status == workflow.RunStatusCompleted {
				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_completed", Payload: statePayload{Vars: termRun.State.Vars}})
			} else {
				_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_error", Payload: map[string]string{
					"error":  termRun.Error,
					"status": string(termRun.Status),
				}})
			}
			return nil
		default:

			_ = s.writeJSON(conn, &writeMu, simServerMsg{Type: "run_completed", Payload: statePayload{Vars: run.State.Vars}})
			return nil
		}
	}
}

func (s *wsWorkflowSimulation) writeJSON(conn *websocket.Conn, mu *sync.Mutex, msg simServerMsg) error {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(msg)
}

func (s *wsWorkflowSimulation) sendError(conn *websocket.Conn, mu *sync.Mutex, errMsg string) error {
	log.Printf("[workflow-sim] error: %s", errMsg)
	return s.writeJSON(conn, mu, simServerMsg{Type: "error", Payload: map[string]string{"error": errMsg}})
}

func (s *wsWorkflowSimulation) newSimulationRegistry(workspaceID, simLeadID string, simMsgRepo *simMessageRepo, outboundCh chan<- SimOutboundMessage) *NodeExecutorRegistry {
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{
		AIService:               s.deps.AIService,
		AgentRepo:               s.deps.AgentRepo,
		MessageRepo:             simMsgRepo,
		HistoryManager:          &simHistoryManager{},
		LeadRepo:                &simLeadRepo{},
		WhatsAppEntryRepo:       &simWhatsAppEntryRepo{leadID: simLeadID},
		BusinessPhoneRepo:       &simBusinessPhoneRepo{workspaceID: workspaceID},
		MessageWindowRepo:       &simMessageWindowRepo{},
		WhatsAppClientFactory:   newSimWhatsAppClientFactory(outboundCh),
		ToolRegistry:            s.deps.ToolRegistry,
		TemplateRepo:            s.deps.TemplateRepo,
		MediaRepo:               s.deps.MediaRepo,
		ConsumeWhatsappTemplate: &simBalanceConsumer{},
		SubWorkflowRunner:       &simSubWorkflowRunner{},
		SharedState:             s.deps.SharedState,
		CalendarRepo:            s.deps.CalendarRepo,
		GoogleCalendar:          s.deps.GoogleCalendar,
		RescheduleEventUC: calendar_usecase.NewRescheduleEventUseCase(
			s.deps.CalendarRepo,
			s.deps.GoogleCalendar,
			calendar_usecase.NewUpdateEventUseCase(s.deps.CalendarRepo, s.deps.GoogleCalendar),
		),
		BillingPub: s.deps.BillingPub,
	})
	return registry
}
