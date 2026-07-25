package workflow_usecase

import (
	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/cache"
	calendar_domain "vozko/domain/calendar"
	"vozko/domain/conversation"
	ia_domain "vozko/domain/inbox_assignment"
	label_domain "vozko/domain/label"
	lead_domain "vozko/domain/lead"
	lead_message_window_domain "vozko/domain/lead_message_window"
	media_domain "vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/rag"
	"vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	template_domain "vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
	workspace_phone_access_domain "vozko/domain/workspace_phone_access"
	email_usecase "vozko/usecases/email"
	"vozko/usecases/workflow/node_executors"
)

type ExecutorDeps struct {
	AIService               ai.Service
	AgentRepo               agent.Repository
	CalendarRepo            calendar_domain.Repository
	GoogleCalendar          calendar_domain.GoogleOAuthService
	RescheduleEventUC       calendar_domain.RescheduleEventUseCase
	MessageRepo             conversation.MessageRepository
	HistoryManager          conversation.MessageHistoryManager
	LeadRepo                lead_domain.Repository
	WhatsAppEntryRepo       wce.Repository
	BusinessPhoneRepo       businessphone.Repository
	MessageWindowRepo       lead_message_window_domain.Repository
	WhatsAppClientFactory   conversation.WhatsAppClientFactory
	ToolRegistry            tools.Service
	TemplateRepo            template_domain.Repository
	MediaRepo               media_domain.MediaRepository
	ConsumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase
	WorkspacePhoneAccess    workspace_phone_access_domain.Repository
	SubWorkflowRunner       node_executors.SubWorkflowRunner
	SharedState             cache.SharedState
	LabelRepo               label_domain.Repository
	DepartmentRepo          dept_domain.Repository
	InboxAssignmentRepo     ia_domain.Repository
	WorkspaceRepo           workspace_domain.Repository
	CachedBalanceChecker    balance.CachedBalanceChecker
	BillingPub              messaging.MessageQueuePub
	RAGService              rag.RAGService
	EmailSender             email_usecase.SMTPSender
	FileStorage             media_domain.FileStorage
	ConversationMediaRepo   conversation.ConversationMediaRepository

	// AIAttendance is optional; when set, WhatsApp workflow AI agent nodes publish sessions.
	AIAttendance node_executors.WorkflowAIAttendance
	// ConversationStatus is the single finish/reopen choke point (shared with AI tools / hub).
	ConversationStatus conversation.ConversationStatusUpdater
}

func RegisterDefaultExecutors(registry *NodeExecutorRegistry, deps ExecutorDeps) {
	waDeps := node_executors.WhatsAppSenderDeps{
		ClientFactory:           deps.WhatsAppClientFactory,
		LeadRepo:                deps.LeadRepo,
		WhatsAppEntryRepo:       deps.WhatsAppEntryRepo,
		BusinessPhoneRepo:       deps.BusinessPhoneRepo,
		MessageWindowRepo:       deps.MessageWindowRepo,
		HistoryManager:          deps.HistoryManager,
		TemplateRepo:            deps.TemplateRepo,
		MediaRepo:               deps.MediaRepo,
		ConsumeWhatsappTemplate: deps.ConsumeWhatsappTemplate,
		WorkspacePhoneAccess:    deps.WorkspacePhoneAccess,
		BillingPub:              deps.BillingPub,
		FileStorage:             deps.FileStorage,
		ConversationMediaRepo:   deps.ConversationMediaRepo,
	}

	emailSender := deps.EmailSender
	if emailSender == nil {
		emailSender = email_usecase.NewGoMailSMTPSender()
	}
	registry.Register(workflow.NodeTypeActionSendText, node_executors.NewSendTextExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSendTemplate, node_executors.NewSendTemplateExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSendEmail, node_executors.NewSendEmailExecutor(emailSender))
	registry.Register(workflow.NodeTypeActionSendMedia, node_executors.NewSendMediaExecutor(waDeps))
	registry.Register(workflow.NodeTypeActionSetVariable, node_executors.NewSetVariableExecutor())
	registry.Register(workflow.NodeTypeActionHTTPRequest, node_executors.NewHTTPRequestExecutor())
	registry.Register(workflow.NodeTypeActionSendWhatsappButton, node_executors.NewSendWhatsappButtonExecutor(waDeps))

	aiAgentExec := node_executors.NewAIAgentExecutor(deps.AIService, deps.AgentRepo, deps.MessageRepo, deps.CachedBalanceChecker, node_executors.NewWhatsAppSenderFromDeps(waDeps), deps.RAGService)
	if deps.AIAttendance != nil {
		node_executors.SetAIAttendanceOnExecutor(aiAgentExec, deps.AIAttendance)
	}
	registry.Register(workflow.NodeTypeActionAIAgent, aiAgentExec)

	registry.Register(workflow.NodeTypeActionScheduleMeeting, node_executors.NewScheduleMeetingExecutor(deps.CalendarRepo, deps.GoogleCalendar))
	registry.Register(workflow.NodeTypeActionRescheduleMeeting, node_executors.NewRescheduleMeetingExecutor(deps.RescheduleEventUC))
	registry.Register(workflow.NodeTypeActionCheckCalendarAvailability, node_executors.NewCheckCalendarAvailabilityExecutor(deps.CalendarRepo, deps.GoogleCalendar))
	registry.Register(workflow.NodeTypeActionAssignLabel, node_executors.NewAssignLabelExecutor(deps.LabelRepo))
	registry.Register(workflow.NodeTypeActionTransferDepartment, node_executors.NewTransferDepartmentExecutor(deps.DepartmentRepo, deps.InboxAssignmentRepo))
	registry.Register(workflow.NodeTypeActionAssignMember, node_executors.NewAssignMemberExecutor(deps.WorkspaceRepo, deps.InboxAssignmentRepo))
	registry.Register(workflow.NodeTypeActionFinishConversation, node_executors.NewFinishConversationExecutor(deps.ConversationStatus))
	registry.Register(workflow.NodeTypeActionFormatDate, node_executors.NewFormatDateExecutor())
	registry.Register(workflow.NodeTypeActionCode, node_executors.NewCodeExecutor())
	registry.Register(workflow.NodeTypeActionGetCurrentTime, node_executors.NewGetCurrentTimeExecutor())
	registry.Register(workflow.NodeTypeActionLoop, node_executors.NewLoopExecutor())
	registry.Register(workflow.NodeTypeActionRunWorkflow, node_executors.NewRunWorkflowExecutor(deps.SubWorkflowRunner))

	registry.Register(workflow.NodeTypeWaitDuration, node_executors.NewWaitDurationExecutor())
	registry.Register(workflow.NodeTypeWaitForReply, node_executors.NewWaitForReplyExecutor())
	registry.Register(workflow.NodeTypeWaitSchedule, node_executors.NewScheduleWaitExecutor())

	registry.Register(workflow.NodeTypeConditionBranch, node_executors.NewConditionBranchExecutor())
	registry.Register(workflow.NodeTypeConditionTextMatch, node_executors.NewTextMatchExecutor())
	registry.Register(workflow.NodeTypeConditionFilter, node_executors.NewFilterExecutor())
	registry.Register(workflow.NodeTypeConditionCheckLabel, node_executors.NewCheckLabelExecutor(deps.LabelRepo))

	registry.Register(workflow.NodeTypeDecorationBackground, node_executors.NewBackgroundNodeExecutor())

	for _, def := range workflow.BuiltinDefinitions() {
		registry.RegisterDefinition(def)
	}
}
