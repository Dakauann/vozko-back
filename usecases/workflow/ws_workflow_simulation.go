package workflow_usecase

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/cache"
	"vozko/domain/calendar"
	"vozko/domain/conversation"
	"vozko/domain/label"
	media_domain "vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	"vozko/domain/tools"
	template_domain "vozko/domain/whatsapp/template"
	"vozko/domain/workflow"
	"vozko/domain/workflowsecret"
	"vozko/domain/workspace"
	"vozko/domain/workspace/workspace_department"
	calendar_usecase "vozko/usecases/calendar"
	"vozko/usecases/workflow/node_executors"
)

type simServerMsg struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type messagePayload struct {
	Direction   string                `json:"direction"`
	Text        string                `json:"text"`
	MsgType     string                `json:"msgType"`
	NodeID      string                `json:"nodeId,omitempty"`
	MessageID   string                `json:"messageId"`
	AudioBase64 string                `json:"audioBase64,omitempty"`
	AudioMime   string                `json:"audioMime,omitempty"`
	AudioURL    string                `json:"audioUrl,omitempty"`
	Message     *conversation.Message `json:"message,omitempty"`
}

type receivedPayload struct {
	Direction string               `json:"direction"`
	Message   conversation.Message `json:"message"`
}

type replyIgnoredPayload struct {
	NodeID string `json:"nodeId"`
	Reason string `json:"reason"`
}

type waitingReplyPayload struct {
	NodeID         string `json:"nodeId"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type statePayload struct {
	Vars map[string]interface{} `json:"vars"`
}

func redactedState(run *workflow.WorkflowRun) statePayload {
	return statePayload{Vars: outbound.Map(run.State.Vars)}
}

func runErrorPayload(message string, status workflow.RunStatus) map[string]string {
	payload := map[string]string{"error": outbound.String(message)}
	if status != "" {
		payload["status"] = string(status)
	}
	return payload
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
	CallQueues     CallQueueLookup
	HTTPEgress     node_executors.HTTPEgress
	MediaFetcher   node_executors.MediaFetcher
	Secrets        workflowsecret.Resolver

	LabelRepo      label.Repository
	StageRepo      node_executors.StageReader
	WorkspaceRepo  workspace.Repository
	DepartmentRepo workspace_department.Repository
	DealPreviews   DealPreviewer
	LeadPreviews   LeadProfilePreviewer

	Runs             workflow.GetRunUseCase
	Permissions      workspace.PermissionChecker
	PinSources       PinSources
	PauseIdleTimeout time.Duration
}

type SimulationSession struct {
	WorkflowID  string
	WorkspaceID string
	Viewer      shared.Person
	AwaitStart  bool
}

type WSWorkflowSimulationUseCase interface {
	HandleSession(ctx context.Context, conn *websocket.Conn, session SimulationSession) error
}

type wsWorkflowSimulation struct {
	deps WSWorkflowSimulationDeps
}

func NewWSWorkflowSimulationUseCase(deps WSWorkflowSimulationDeps) WSWorkflowSimulationUseCase {
	return &wsWorkflowSimulation{deps: deps}
}

func (s *wsWorkflowSimulation) HandleSession(ctx context.Context, conn *websocket.Conn, session SimulationSession) error {
	var writeMu sync.Mutex
	wf, trigger, err := s.loadSimWorkflow(session)
	if err != nil {
		return s.sendError(conn, &writeMu, err.Error())
	}
	if wf.Type == workflow.WorkflowTypeVoice {
		return s.runVoiceSimulation(ctx, conn, &writeMu, wf, trigger, session)
	}
	sim := s.newMessagingSim(ctx, conn, &writeMu, session, wf, trigger)
	defer close(sim.outbound)
	if err := sim.runs.Create(sim.run); err != nil {
		return s.sendError(conn, &writeMu, "failed to create sim run: "+err.Error())
	}
	return sim.play()
}

func (s *wsWorkflowSimulation) writeJSON(conn *websocket.Conn, mu *sync.Mutex, msg simServerMsg) error {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(msg)
}

func (s *wsWorkflowSimulation) sendCancelled(conn *websocket.Conn, mu *sync.Mutex, reason string) {
	_ = s.writeJSON(conn, mu, simServerMsg{Type: "run_cancelled", Payload: map[string]string{"reason": reason}})
}

func (s *wsWorkflowSimulation) refuseCommand(conn *websocket.Conn, mu *sync.Mutex, command string, err error) {
	_ = s.writeJSON(conn, mu, simServerMsg{Type: "command_refused", Payload: map[string]string{"command": command, "error": err.Error()}})
}

func (s *wsWorkflowSimulation) sendError(conn *websocket.Conn, mu *sync.Mutex, errMsg string) error {
	log.Printf("[workflow-sim] error: %s", errMsg)
	return s.writeJSON(conn, mu, simServerMsg{Type: "error", Payload: map[string]string{"error": errMsg}})
}

func (s *wsWorkflowSimulation) newSimulationRegistry(box SimSandbox, voice workflow.VoiceAudio) *NodeExecutorRegistry {
	return newSimulationRegistry(ExecutorDeps{
		VoiceAudio:     voice,
		AIService:      s.deps.AIService,
		AgentRepo:      s.deps.AgentRepo,
		ToolRegistry:   s.deps.ToolRegistry,
		TemplateRepo:   s.deps.TemplateRepo,
		MediaRepo:      s.deps.MediaRepo,
		SharedState:    s.deps.SharedState,
		CalendarRepo:   s.deps.CalendarRepo,
		GoogleCalendar: s.deps.GoogleCalendar,
		RescheduleEventUC: calendar_usecase.NewRescheduleEventUseCase(
			s.deps.CalendarRepo,
			s.deps.GoogleCalendar,
			calendar_usecase.NewUpdateEventUseCase(s.deps.CalendarRepo, s.deps.GoogleCalendar),
		),
		BillingPub:     s.deps.BillingPub,
		HTTPEgress:     s.deps.HTTPEgress,
		MediaFetcher:   s.deps.MediaFetcher,
		Secrets:        s.deps.Secrets,
		LabelRepo:      s.deps.LabelRepo,
		StageRepo:      s.deps.StageRepo,
		WorkspaceRepo:  s.deps.WorkspaceRepo,
		DepartmentRepo: s.deps.DepartmentRepo,
		DealPreviews:   s.deps.DealPreviews,
		LeadPreviews:   s.deps.LeadPreviews,
	}, box)
}
