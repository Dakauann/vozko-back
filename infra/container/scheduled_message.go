package container

import (
	"log"

	scheduled_message_domain "vozko/domain/scheduled_message"
	whatsapp_outreach_domain "vozko/domain/whatsapp_outreach"
	scheduled_message_usecase "vozko/usecases/scheduled_message"
	workspace_usecase "vozko/usecases/workspace"
)

type scheduledMessageUseCases struct {
	schedule   scheduled_message_domain.ScheduleUseCase
	reschedule scheduled_message_domain.RescheduleUseCase
	cancel     scheduled_message_domain.CancelUseCase
	person     scheduled_message_domain.PersonSchedulerUseCase
	list       scheduled_message_domain.ListUseCase
	dispatch   scheduled_message_domain.DispatchUseCase
	consume    scheduled_message_domain.ConsumeFireUseCase
	sweep      scheduled_message_domain.SweepJob
	purge      scheduled_message_domain.PurgeJob
}

func (c *Container) buildScheduledMessages(templates whatsapp_outreach_domain.ConversationTemplateUseCase) scheduledMessageUseCases {
	clock := scheduled_message_domain.SystemClock{}
	repo := c.repositories.scheduledMessage

	wake, err := scheduled_message_usecase.NewQueueWakeScheduler(c.services.scheduledMsgQueuePub)
	if err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}

	windows := c.services.conversationHistory
	permissions := workspace_usecase.NewCheckAccessUseCase(c.repositories.workspace)

	var built scheduledMessageUseCases

	if built.schedule, err = scheduled_message_usecase.NewScheduleUseCase(repo, windows, templates, wake, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.reschedule, err = scheduled_message_usecase.NewRescheduleUseCase(repo, windows, wake, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.cancel, err = scheduled_message_usecase.NewCancelUseCase(repo); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	built.person = scheduled_message_usecase.NewPersonSchedulerUseCase(c.services.conversationAuth, permissions, repo, built.schedule, built.reschedule, built.cancel)
	if built.list, err = scheduled_message_usecase.NewListUseCase(repo, windows, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}

	if built.dispatch, err = scheduled_message_usecase.NewDispatchUseCase(
		repo, windows, c.services.liveOperatorSend, templates, permissions, c.services.conversationHub, clock,
	); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}

	if built.consume, err = scheduled_message_usecase.NewConsumeFireUseCase(
		c.services.scheduledMsgQueueSub, built.dispatch,
	); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.sweep, err = scheduled_message_usecase.NewSweepJob(repo, built.dispatch, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.purge, err = scheduled_message_usecase.NewPurgeJob(repo, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}

	return built
}
