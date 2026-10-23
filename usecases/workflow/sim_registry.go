package workflow_usecase

import (
	"context"
	"errors"

	"vozko/domain/media"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

func newSandboxEngine(runs workflow.WorkflowRunRepository, logs workflow.WorkflowRunLogRepository, registry *NodeExecutorRegistry, steps workflow.StepGate) *RunEngine {
	engine := NewRunEngine(runs, logs, registry)
	engine.SetStepGate(steps)
	engine.SetAutomationGate(SandboxAutomationGate{})
	return engine
}

var errSandboxMediaNotFetched = errors.New("the test does not download media without a real-call confirmation; the message carries the link")

type simMediaFetcher struct {
	confirmed func() bool
	guarded   node_executors.MediaFetcher
}

func (f simMediaFetcher) FetchMedia(ctx context.Context, mediaURL string) ([]byte, string, error) {
	if f.confirmed == nil || !f.confirmed() {
		return nil, "", errSandboxMediaNotFetched
	}
	if f.guarded == nil {
		return nil, "", media.ErrRemoteFilesUnwired
	}
	return f.guarded.FetchMedia(ctx, mediaURL)
}

type SimSandbox struct {
	WorkspaceID        string
	LeadID             string
	Messages           *simMessageRepo
	Outbound           func(SimOutboundMessage)
	Window             *simMessageWindowRepo
	Effects            *simEffects
	RealCallsConfirmed func() bool
}

func newSimulationRegistry(deps ExecutorDeps, box SimSandbox) *NodeExecutorRegistry {
	registry := NewNodeExecutorRegistry()
	effects := box.Effects
	if effects == nil {
		effects = newSimEffects()
	}
	channel := newSimChannel(box.Outbound, effects)
	simClients := newSimWhatsAppClientFactory(channel)
	handOff := simHandOff{members: deps.WorkspaceRepo, departments: deps.DepartmentRepo, effects: effects}
	RegisterDefaultExecutors(registry, ExecutorDeps{
		AIService:             deps.AIService,
		AgentRepo:             deps.AgentRepo,
		MessageRepo:           box.Messages,
		HistoryManager:        channel,
		ConversationEvents:    simulationEvents,
		LeadRepo:              &simLeadRepo{},
		WhatsAppEntryRepo:     &simWhatsAppEntryRepo{leadID: box.LeadID},
		BusinessPhoneRepo:     &simBusinessPhoneRepo{workspaceID: box.WorkspaceID},
		MessageWindowRepo:     box.window(),
		WhatsAppClientFactory: simClients,
		ToolRegistry:          deps.ToolRegistry,
		TemplateRepo:          deps.TemplateRepo,
		MediaRepo:             deps.MediaRepo,
		MediaURLs:             deps.MediaURLs,
		TemplateSends:         &simTemplateSends{client: simClients.client, templates: deps.TemplateRepo},
		SubWorkflowRunner:     &simSubWorkflowRunner{},
		SharedState:           deps.SharedState,
		CalendarRepo:          deps.CalendarRepo,
		GoogleCalendar:        deps.GoogleCalendar,
		RescheduleEventUC:     deps.RescheduleEventUC,
		BillingPub:            deps.BillingPub,
		EmailSender:           deps.EmailSender,
		VoiceAudio:            deps.VoiceAudio,
		HTTPEgress:            deps.HTTPEgress,
		MediaFetcher:          simMediaFetcher{confirmed: box.RealCallsConfirmed, guarded: deps.MediaFetcher},
		Secrets:               deps.Secrets,
		Forms:                 newSimFormSender(channel),
		LabelRepo:             deps.LabelRepo,
		Labeler:               simLabeler{labels: deps.LabelRepo, effects: effects},
		StageRepo:             deps.StageRepo,
		AssignStage:           simStageMover{stages: deps.StageRepo, effects: effects},
		Deals:                 simDeals{previews: deps.DealPreviews, effects: effects},
		LeadProfiles:          simLeadProfiles{previews: deps.LeadPreviews, effects: effects},
		WorkspaceRepo:         deps.WorkspaceRepo,
		DepartmentRepo:        deps.DepartmentRepo,
		ConversationHandOff:   handOff,
	})
	return registry
}

func (box SimSandbox) window() *simMessageWindowRepo {
	if box.Window == nil {
		return newSimMessageWindow(box.LeadID, false)
	}
	return box.Window
}
