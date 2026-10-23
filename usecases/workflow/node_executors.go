package workflow_usecase

import (
	"errors"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/cache"
	calendar_domain "vozko/domain/calendar"
	"vozko/domain/carriermessaging"
	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	label_domain "vozko/domain/label"
	lead_domain "vozko/domain/lead"
	lead_message_window_domain "vozko/domain/lead_message_window"
	media_domain "vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/rag"
	stage_domain "vozko/domain/stage"
	"vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	template_domain "vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	"vozko/domain/workflowsecret"
	workspace_domain "vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
	workspace_phone_access_domain "vozko/domain/workspace_phone_access"
	email_usecase "vozko/usecases/email"
	"vozko/usecases/transactionalemail"
	"vozko/usecases/workflow/node_executors"
)

type ExecutorDeps struct {
	AIService             ai.Service
	AgentRepo             agent.Repository
	CalendarRepo          calendar_domain.Repository
	GoogleCalendar        calendar_domain.GoogleCalendarAPI
	CalendarAccess        calendar_domain.AccessProvider
	RescheduleEventUC     calendar_domain.RescheduleEventUseCase
	MessageRepo           conversation.MessageRepository
	HistoryManager        conversation.MessageHistoryManager
	LeadRepo              lead_domain.Repository
	EntryLeads            lead_domain.EntryLeads
	WhatsAppEntryRepo     wce.Repository
	BusinessPhoneRepo     businessphone.Repository
	MessageWindowRepo     lead_message_window_domain.Repository
	WhatsAppClientFactory conversation.WhatsAppClientFactory
	ToolRegistry          tools.Service
	TemplateRepo          template_domain.Repository
	MediaRepo             media_domain.MediaRepository
	MediaURLs             *conversation.MediaURLResolver
	TemplateSends         template_domain.BilledTemplateSendUseCase
	WorkspacePhoneAccess  workspace_phone_access_domain.Repository
	SubWorkflowRunner     node_executors.SubWorkflowRunner
	SharedState           cache.SharedState
	LabelRepo             label_domain.Repository
	Labeler               label_domain.AutomationLabeler
	StageRepo             node_executors.StageReader
	AssignStage           stage_domain.AssignEntryStageUseCase
	StageBroadcaster      node_executors.StageBroadcaster
	Deals                 node_executors.DealDesk
	DealPreviews          DealPreviewer
	LeadProfiles          node_executors.LeadProfileWriter
	LeadPreviews          LeadProfilePreviewer
	Consent               node_executors.ConsentRecorder
	DepartmentRepo        dept_domain.Repository
	ConversationHandOff   node_executors.ConversationHandOff
	WorkspaceRepo         workspace_domain.Repository
	CachedBalanceChecker  balance.CachedBalanceChecker
	BillingPub            messaging.MessageQueuePub
	RAGService            rag.RAGService
	EmailSender           email_usecase.SMTPSender
	FileStorage           media_domain.FileStorage
	ConversationMediaRepo conversation.ConversationMediaRepository
	VoiceAudio            workflow.VoiceAudio
	HTTPEgress            node_executors.HTTPEgress
	MediaFetcher          node_executors.MediaFetcher
	Secrets               workflowsecret.Resolver
	Forms                 node_executors.FormSender
	Carrier               node_executors.CarrierSender
	PlatformEmail         transactionalemail.Sender
	SheetRows             node_executors.SheetRowSink

	AIAttendance       node_executors.WorkflowAIAttendance
	ConversationStatus conversation.ConversationStatusUpdater
	Adapters           conversation.AdapterRegistry
	ConversationEvents ce.Logger
	ChoiceStats        choicestats.Recorder
}

var ErrConversationEventsRequired = errors.New("workflow executors: the conversation event logger is required to record refused sends")

var ErrFormSenderRequired = errors.New("workflow executors: the whatsapp form sender is required to send forms")

var ErrChoiceRecorderRequired = errors.New("workflow executors: the choice click-through recorder is required to count prompt sends")

var ErrMediaURLsRequired = errors.New("workflow executors: the media URL resolver is required to check media sent by URL")

type discardedEvents struct{}

func (discardedEvents) Log(*ce.ConversationEvent) {}

var simulationEvents ce.Logger = discardedEvents{}

func RegisterProductionExecutors(registry *NodeExecutorRegistry, deps ExecutorDeps) error {
	if deps.ConversationEvents == nil {
		return ErrConversationEventsRequired
	}
	if deps.Forms == nil {
		return ErrFormSenderRequired
	}
	if deps.ChoiceStats == nil {
		return ErrChoiceRecorderRequired
	}
	if deps.MediaURLs == nil {
		return ErrMediaURLsRequired
	}
	RegisterDefaultExecutors(registry, deps)
	return nil
}

func RegisterDefaultExecutors(registry *NodeExecutorRegistry, deps ExecutorDeps) {
	waDeps := node_executors.SenderDeps{
		ClientFactory:         deps.WhatsAppClientFactory,
		LeadRepo:              deps.LeadRepo,
		WhatsAppEntryRepo:     deps.WhatsAppEntryRepo,
		BusinessPhoneRepo:     deps.BusinessPhoneRepo,
		MessageWindowRepo:     deps.MessageWindowRepo,
		HistoryManager:        deps.HistoryManager,
		TemplateRepo:          deps.TemplateRepo,
		MediaRepo:             deps.MediaRepo,
		MediaURLs:             deps.MediaURLs,
		TemplateSends:         deps.TemplateSends,
		WorkspacePhoneAccess:  deps.WorkspacePhoneAccess,
		BillingPub:            deps.BillingPub,
		FileStorage:           deps.FileStorage,
		ConversationMediaRepo: deps.ConversationMediaRepo,
		Adapters:              deps.Adapters,
		EntryLeads:            deps.EntryLeads,
		Events:                deps.ConversationEvents,
		MediaFetcher:          deps.MediaFetcher,
		ChoiceStats:           deps.ChoiceStats,
	}

	emailSender := deps.EmailSender
	if emailSender == nil {
		emailSender = email_usecase.NewGoMailSMTPSender()
	}
	registry.Register(workflow.NodeTypeActionSendText, node_executors.NewSendTextExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSendTemplate, node_executors.NewSendTemplateExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSendEmail, node_executors.NewSendEmailExecutor(emailSender))
	if len(carriermessaging.LaunchedChannels()) > 0 {
		registry.Register(workflow.NodeTypeActionSendCarrier, node_executors.NewSendCarrierExecutor(node_executors.CarrierNodeDeps{Sender: deps.Carrier, Leads: deps.LeadRepo, EntryLeads: deps.EntryLeads}))
	}
	registry.Register(workflow.NodeTypeActionSendPlatformEmail, node_executors.NewSendPlatformEmailExecutor(node_executors.PlatformEmailNodeDeps{Sender: deps.PlatformEmail, EntryLeads: deps.EntryLeads}))
	registry.Register(workflow.NodeTypeActionSendMedia, node_executors.NewSendMediaExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSetVariable, node_executors.NewSetVariableExecutor())
	registry.Register(workflow.NodeTypeActionHTTPRequest, node_executors.NewHTTPRequestExecutor(deps.HTTPEgress, deps.Secrets))
	registry.Register(workflow.NodeTypeActionSendInteractive, node_executors.NewInteractivePromptExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSendForm, node_executors.NewSendFormExecutor(deps.Forms, waDeps))

	aiAgentExec := node_executors.NewAIAgentExecutor(deps.AIService, deps.AgentRepo, deps.MessageRepo, deps.CachedBalanceChecker, node_executors.NewWhatsAppSenderFromDeps(waDeps), deps.RAGService)
	if deps.AIAttendance != nil {
		node_executors.SetAIAttendanceOnExecutor(aiAgentExec, deps.AIAttendance)
	}
	registry.Register(workflow.NodeTypeActionAIAgent, aiAgentExec)

	registry.Register(workflow.NodeTypeActionCreateCalendarEvent, node_executors.NewCreateCalendarEventExecutor(deps.CalendarRepo, deps.GoogleCalendar, deps.CalendarAccess))
	registry.Register(workflow.NodeTypeActionRescheduleCalendarEvent, node_executors.NewRescheduleCalendarEventExecutor(deps.RescheduleEventUC))
	registry.Register(workflow.NodeTypeActionCheckCalendarAvailability, node_executors.NewCheckCalendarAvailabilityExecutor(deps.CalendarRepo, deps.GoogleCalendar, deps.CalendarAccess))
	registry.Register(workflow.NodeTypeActionAddSheetRow, node_executors.NewAddSheetRowExecutor(deps.SheetRows))
	registry.Register(workflow.NodeTypeActionAssignLabel, node_executors.NewAssignLabelExecutor(deps.Labeler))
	registry.Register(workflow.NodeTypeActionMoveStage, node_executors.NewMoveStageExecutor(deps.StageRepo, deps.AssignStage, deps.StageBroadcaster))
	registry.Register(workflow.NodeTypeActionManageOpportunity, node_executors.NewManageOpportunityExecutor(deps.Deals))
	registry.Register(workflow.NodeTypeActionUpdateLead, node_executors.NewUpdateLeadExecutor(deps.LeadProfiles))
	consentDeps := node_executors.ConsentNodeDeps{Consent: deps.Consent, Messages: deps.MessageRepo, EntryLeads: deps.EntryLeads, Leads: deps.LeadRepo}
	registry.Register(workflow.NodeTypeActionConsent, node_executors.NewConsentExecutor(consentDeps))
	registry.Register(workflow.NodeTypeConditionConsent, node_executors.NewCheckConsentExecutor(consentDeps))
	registry.Register(workflow.NodeTypeActionTransferDepartment, node_executors.NewTransferDepartmentExecutor(deps.DepartmentRepo, deps.WorkspaceRepo, deps.ConversationHandOff))
	registry.Register(workflow.NodeTypeActionAssignMember, node_executors.NewAssignMemberExecutor(deps.WorkspaceRepo, deps.ConversationHandOff))
	registry.Register(workflow.NodeTypeActionFinishConversation, node_executors.NewFinishConversationExecutor(deps.ConversationStatus))
	registry.Register(workflow.NodeTypeActionFormatDate, node_executors.NewFormatDateExecutor())
	registry.Register(workflow.NodeTypeActionCode, node_executors.NewCodeExecutor(deps.Secrets))
	registry.Register(workflow.NodeTypeActionGetCurrentTime, node_executors.NewGetCurrentTimeExecutor())
	registry.Register(workflow.NodeTypeActionLoop, node_executors.NewLoopExecutor())
	registry.Register(workflow.NodeTypeActionRunWorkflow, node_executors.NewRunWorkflowExecutor(deps.SubWorkflowRunner))
	registry.Register(workflow.NodeTypeActionReplyToAgent, node_executors.NewReplyToAgentExecutor())

	registry.Register(workflow.NodeTypeWaitDuration, node_executors.NewWaitDurationExecutor())
	registry.Register(workflow.NodeTypeWaitForReply, node_executors.NewWaitForReplyExecutor())
	registry.Register(workflow.NodeTypeWaitSchedule, node_executors.NewScheduleWaitExecutor())

	registry.Register(workflow.NodeTypeConditionBranch, node_executors.NewConditionBranchExecutor())
	registry.Register(workflow.NodeTypeConditionChannel, node_executors.NewChannelBranchExecutor())
	registry.Register(workflow.NodeTypeConditionTextMatch, node_executors.NewTextMatchExecutor())
	registry.Register(workflow.NodeTypeConditionFilter, node_executors.NewFilterExecutor())
	registry.Register(workflow.NodeTypeConditionCheckLabel, node_executors.NewCheckLabelExecutor(deps.LabelRepo))
	registry.Register(workflow.NodeTypeConditionCheckStage, node_executors.NewCheckStageExecutor(deps.StageRepo))
	registry.Register(workflow.NodeTypeConditionCheckOpportunity, node_executors.NewCheckOpportunityExecutor(deps.Deals))
	registry.Register(workflow.NodeTypeActionPlayAudio, node_executors.NewPlayAudioExecutor(deps.VoiceAudio))
	registry.Register(workflow.NodeTypeWaitDTMF, node_executors.NewWaitDTMFExecutor())
	registry.Register(workflow.NodeTypeTransferToQueue, node_executors.NewTransferToQueueExecutor())

	registry.Register(workflow.NodeTypeDecorationBackground, node_executors.NewBackgroundNodeExecutor())

	for _, def := range workflow.BuiltinDefinitions() {
		registry.RegisterDefinition(def)
	}
}
