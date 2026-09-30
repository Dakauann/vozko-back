package container

import (
	"log"
	"net/http"

	callroutinghttp "vozko/delivery/http/callrouting"
	wsdelivery "vozko/delivery/ws"
	callsession_domain "vozko/domain/callsession"
	workspace_domain "vozko/domain/workspace"
	callrouting_infra "vozko/infra/callrouting"
	"vozko/infra/holdmusic"
	media_infra "vozko/infra/media"
	callrouting_repository "vozko/infra/repositories/callrouting"
	callrouting_usecase "vozko/usecases/callrouting"
	callsession_usecase "vozko/usecases/callsession"
	conversation_usecase "vozko/usecases/conversation"
)

type callRoutingBundle struct {
	Broker     *callsession_usecase.InboundOfferBroker
	Channels   *wsdelivery.CallChannels
	Dispatcher *callrouting_usecase.Dispatcher
	Transfers  *callrouting_usecase.TransferCall
	Queues     *callrouting_repository.QueueRepository
	VoiceAudio *media_infra.VoiceAudioLoader
	Handler    *callroutinghttp.Handler
}

type callAnswerPermission struct {
	trunks trunkCallPermissions
	access workspace_domain.CheckAccessUseCase
}

func (p callAnswerPermission) MayAnswerCalls(userID, workspaceID, channel string) bool {
	switch channel {
	case callsession_domain.OfferChannelSIP:
		return p.trunks.MayCallThroughTrunks(userID, workspaceID, false)
	case callsession_domain.OfferChannelWhatsApp:
		return p.allowed(userID, workspaceID, workspace_domain.ResourceCallSession, workspace_domain.ActionUse) &&
			p.allowed(userID, workspaceID, workspace_domain.ResourceConversations, workspace_domain.ActionRead)
	}
	return false
}

func (p callAnswerPermission) MayTransferCalls(userID, workspaceID string) bool {
	return p.allowed(userID, workspaceID, workspace_domain.ResourceCallSession, workspace_domain.ActionTransfer)
}

func (p callAnswerPermission) allowed(userID, workspaceID string, resource workspace_domain.Resource, action workspace_domain.Action) bool {
	return p.access.Execute(userID, workspaceID, resource, action) == nil
}

func (c *Container) initCallRouting() {
	voiceAudio := media_infra.NewVoiceAudioLoader(c.repositories.media, &http.Client{Timeout: voiceAudioDownloadTimeout})
	library, err := holdmusic.NewLibrary(voiceAudio)
	if err != nil {
		log.Fatalf("Failed to load the hold music library: %v", err)
	}
	activity := callrouting_infra.NewAgentActivity()
	broker := callsession_usecase.NewInboundOfferBroker()
	queues := callrouting_repository.NewQueueRepository(c.db)
	settings := callrouting_usecase.NewRoutingSettings(callrouting_repository.NewSettingsRepository(c.db), library)
	channels := wsdelivery.NewCallChannels(activity, c.services.callLifecycle, c.services.endOutboundCall, c.recordingPool, log.Default())
	transferLog := callrouting_repository.NewTransferLog(c.db)
	permission := callAnswerPermission{trunks: c.sipTrunks.Permissions, access: c.sipTrunks.Permissions.access}
	dispatcher := callrouting_usecase.NewDispatcher(callrouting_usecase.DispatcherDeps{
		Sessions:   c.services.callSessions,
		Permission: permission,
		Ringer:     callsession_usecase.NewInboundRinger(broker),
		Activity:   activity,
		Members:    callrouting_usecase.NewQueueMembers(c.repositories.workspaceDepartment),
		Music:      library,
		Logger:     log.Default(),
	})
	c.callRouting = &callRoutingBundle{
		Broker:     broker,
		Channels:   channels,
		Dispatcher: dispatcher,
		Queues:     queues,
		VoiceAudio: voiceAudio,
		Transfers: callrouting_usecase.NewTransferCall(callrouting_usecase.TransferDeps{
			Calls:      channels,
			Queues:     queues,
			Dispatcher: dispatcher,
			Ringer:     callsession_usecase.NewInboundRinger(broker),
			Music:      library,
			HoldMusic:  settings,
			Log:        transferLog,
			Names:      c.services.callSessionUsernameResolver,
			Permission: permission,
			Logger:     log.Default(),
		}),
		Handler: callroutinghttp.NewHandler(callroutinghttp.HandlerDeps{
			Queues: callrouting_usecase.NewQueueCatalog(callrouting_usecase.QueueCatalogDeps{
				Queues:      queues,
				Departments: c.repositories.workspaceDepartment,
				Members:     c.repositories.workspace,
				Music:       library,
			}),
			Settings: settings,
			Targets:  callrouting_usecase.NewTransferTargets(queues, dispatcher),
			Monitor: callrouting_usecase.NewQueueMonitor(callrouting_usecase.QueueMonitorDeps{
				Queues:     queues,
				Dispatcher: dispatcher,
				Names:      c.services.callSessionUsernameResolver,
				History:    transferLog,
			}),
			Music: library,
		}),
	}
}

func callRoutingHandler(c *Container) *callroutinghttp.Handler {
	if c.callRouting == nil {
		return nil
	}
	return c.callRouting.Handler
}

func (c *Container) handCallConversationsOver() {
	c.callRouting.Dispatcher.SetConversationHandoff(conversation_usecase.NewCallConversationHandoff(conversation_usecase.CallConversationHandoffDeps{
		Phones:  c.repositories.businessPhone,
		Entries: c.repositories.wcEntry,
		Assign:  c.services.personAssign,
	}))
}
