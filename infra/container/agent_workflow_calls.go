package container

import (
	"log"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/tools"
	conversation_repository "vozko/infra/repositories/conversation"
	workflow_repository "vozko/infra/repositories/workflow"
	workflow_infra "vozko/infra/workflow"
	"vozko/usecases/agentturn"
	conversation_usecase "vozko/usecases/conversation"
	tools_usecase "vozko/usecases/tools"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) wireAgentWorkflowCalls(steps *workflow_usecase.NodeExecutorRegistry, turnAssembler *agentturn.Assembler) {
	registrar, ok := c.services.toolRegistry.(interface{ Register(tools.Handler) })
	if !ok {
		log.Fatalf("[agent-workflow] the agent tool registry cannot take the run_workflow tool")
	}
	tool := tools_usecase.NewRunWorkflowToolUseCase(c.repositories.workflow, c.tapWorkflows(), steps)
	if tool == nil {
		log.Fatalf("[agent-workflow] the run_workflow tool is missing a dependency")
	}
	registrar.Register(tool)

	runs := workflow_repository.NewAgentCallRunsRepository(c.db)
	status, err := workflow_usecase.NewAgentCallStatus(runs, steps)
	if err != nil {
		log.Fatalf("[agent-workflow] agent run status: %v", err)
	}
	turnAssembler.TrackAgentRuns(status)

	canceller, err := workflow_usecase.NewAgentRunFinishCanceller(runs, c.repositories.workflowRunLog)
	if err != nil {
		log.Fatalf("[agent-workflow] cancelling agent runs on finish: %v", err)
	}
	if c.services.conversationStatusService == nil {
		log.Fatalf("[agent-workflow] the conversation status service is read before it is built")
	}
	c.services.conversationStatusService.SetAgentRunCanceller(canceller)

	names := c.actorNames()
	channels := c.mustChannelAIReply()
	channels.SetWorkflowNames(names)
	whatsapp, ok := c.useCases.handleWhatsAppMessage.(interface {
		SetWorkflowNames(conversation_usecase.ActorNameResolver)
		SetAgentTurnLock(*conversation_usecase.AgentTurnLock)
		conversation_usecase.AgentTurnContinuer
	})
	if !ok {
		log.Fatalf("[agent-workflow] the official WhatsApp reply path cannot label workflow messages or continue an agent turn")
	}
	whatsapp.SetWorkflowNames(names)

	sharedState := c.redisProvider.SharedState()
	if _, guarded := sharedState.(cache.OwnedKeyDeleter); !guarded {
		log.Fatalf("[agent-workflow] the shared state cannot release a turn lock only for its holder")
	}
	turnLock := conversation_usecase.NewAgentTurnLock(sharedState)
	whatsapp.SetAgentTurnLock(turnLock)
	channels.SetAgentTurnLock(turnLock)
	channels.SetAIAttendance(c.services.aiAttendanceService)
	channels.SetEntryLeads(c.repositories.lead)

	customerSpeech, ok := c.repositories.conversation.(conversation.CustomerSpeech)
	if !ok {
		log.Fatalf("[agent-workflow] the message repository cannot tell whether the customer wrote after a run")
	}
	continuation, err := conversation_usecase.NewAgentContinuation(conversation_usecase.AgentContinuationDeps{
		Automations: conversation_repository.NewEntryAutomationReader(c.db),
		Runs:        runs,
		Claims:      runs,
		Messages:    customerSpeech,
		Continuers:  conversation_usecase.AgentTurnContinuers{Official: whatsapp, Channels: channels},
		Lock:        turnLock,
		Metrics:     c.services.metrics,
	})
	if err != nil {
		log.Fatalf("[agent-workflow] agent continuation: %v", err)
	}
	c.useCases.consumeAgentRunFinished = conversation_usecase.NewConsumeAgentRunFinished(c.services.workflowWakeSub, continuation)
	c.useCases.agentContinuationSweep = workflow_usecase.NewAgentContinuationSweep(runs, workflow_infra.NewAgentRunFinishedQueue(c.services.workflowWakePub))
}
