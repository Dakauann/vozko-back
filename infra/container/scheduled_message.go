package container

import (
	"log"

	scheduled_message_domain "vozko/domain/scheduled_message"
	scheduled_message_usecase "vozko/usecases/scheduled_message"
)

// scheduledMessageUseCases is the feature's whole use-case surface.
//
// Returned as a value and spread into the container's useCases literal rather
// than assigned onto it field by field. That is not style: initUseCases builds
// its struct in ONE literal near the end, so anything that writes
// `c.useCases.x = …` from a helper called earlier dereferences a nil pointer.
// Handing the fields back makes the construction order a compile-time fact.
type scheduledMessageUseCases struct {
	schedule   scheduled_message_domain.ScheduleUseCase
	reschedule scheduled_message_domain.RescheduleUseCase
	cancel     scheduled_message_domain.CancelUseCase
	list       scheduled_message_domain.ListUseCase
	dispatch   scheduled_message_domain.DispatchUseCase
	consume    scheduled_message_domain.ConsumeFireUseCase
	sweep      scheduled_message_domain.SweepJob
	purge      scheduled_message_domain.PurgeJob
}

// buildScheduledMessages wires the scheduled-message feature.
//
// Its inputs all come from initConversationSenders, which runs earlier in
// initUseCases: the live operator-send wrapper, the conversation history
// provider and the hub. It must therefore be called after that and before the
// useCases literal it feeds.
//
// Every constructor validates its own required dependencies and returns an
// error, so a mis-wire stops the boot rather than silently costing operators a
// feature. log.Fatalf matches how the rest of the container treats a
// configuration fault it cannot recover from.
func (c *Container) buildScheduledMessages() scheduledMessageUseCases {
	clock := scheduled_message_domain.SystemClock{}
	repo := c.repositories.scheduledMessage

	// Without a broker the sweep still delivers everything, a minute late — but
	// that is a degraded mode, not a supported configuration, so a missing
	// publisher is fatal rather than quietly tolerated.
	wake, err := scheduled_message_usecase.NewQueueWakeScheduler(c.services.scheduledMsgQueuePub)
	if err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}

	// conversationHistory is the channel-agnostic window reader: it already
	// routes WhatsApp to its lead message windows and every other channel to
	// that channel's adapter. Reusing it is what keeps ONE definition of "can we
	// send right now" behind the composer, the send path and this feature.
	windows := c.services.conversationHistory

	var built scheduledMessageUseCases

	if built.schedule, err = scheduled_message_usecase.NewScheduleUseCase(repo, windows, wake, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.reschedule, err = scheduled_message_usecase.NewRescheduleUseCase(repo, windows, wake, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.cancel, err = scheduled_message_usecase.NewCancelUseCase(repo); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}
	if built.list, err = scheduled_message_usecase.NewListUseCase(repo, windows, clock); err != nil {
		log.Fatalf("[container] scheduled messages: %v", err)
	}

	// The dispatcher takes the LIVE operator-send wrapper rather than the
	// concrete use case, for the same reason the hub does: it is constructed
	// inside the ring that wrapper exists to break.
	if built.dispatch, err = scheduled_message_usecase.NewDispatchUseCase(
		repo, windows, c.services.liveOperatorSend, c.services.conversationHub, clock,
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
