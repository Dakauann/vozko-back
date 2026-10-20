package container

import (
	"log"

	"vozko/delivery/http/teamchathttp"
	"vozko/domain/teamchat"
	cronPackage "vozko/infra/cron"
	teamchat_repository "vozko/infra/repositories/teamchat"
	"vozko/usecases/shared/delayedfire"
	teamchat_usecase "vozko/usecases/teamchat"
)

func (c *Container) teamChat() *teamchat_usecase.Service {
	if c.teamChatService != nil {
		return c.teamChatService
	}
	if c.services.conversationHub == nil || c.services.scheduledMsgQueuePub == nil || c.services.conversationAuth == nil {
		log.Fatalf("[team-chat] the conversation hub, the authorizer and the scheduled message queue must exist first")
	}
	wake, err := delayedfire.NewScheduler(c.services.scheduledMsgQueuePub, teamchat.FireTopic)
	if err != nil {
		log.Fatalf("[team-chat] %v", err)
	}
	service, err := teamchat_usecase.New(teamchat_usecase.Deps{
		Chats:     teamchat_repository.New(c.db),
		Access:    c.services.conversationAuth,
		Members:   c.repositories.workspace,
		People:    c.repositories.user,
		Announcer: c.services.conversationHub,
		Wake:      wake,
	})
	if err != nil {
		log.Fatalf("[team-chat] %v", err)
	}
	c.services.conversationHub.SetTeamChat(service)
	c.teamChatService = service
	return service
}

func (c *Container) teamChatHandler() *teamchathttp.Handler {
	return teamchathttp.NewHandler(c.teamChat())
}

func (c *Container) teamChatSweep() cronPackage.CtxJobFunc {
	return cronPackage.CtxJobFunc(c.teamChat().SweepScheduled)
}

func (c *Container) startTeamChatDelivery() {
	consumer, err := delayedfire.NewConsumer(c.services.scheduledMsgQueueSub, teamchat.FireTopic, "team-chat", c.teamChat().DispatchScheduled)
	if err != nil {
		log.Fatalf("[team-chat] %v", err)
	}
	if err := consumer.Start(); err != nil {
		log.Printf("[team-chat] the scheduled message consumer did not start: %v; the minute sweep delivers them", err)
	}
}
