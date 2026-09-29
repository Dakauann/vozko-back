package container

import (
	"context"
	"log"

	conversation_domain "vozko/domain/conversation"
	"vozko/domain/decision"
	"vozko/infra/ai/aibilling"
	"vozko/infra/ai/typesafe"
	livedecision_repository "vozko/infra/repositories/livedecision"
	balance_usecase "vozko/usecases/balance"
	conversation_usecase "vozko/usecases/conversation"
	livedecisions_usecase "vozko/usecases/livedecisions"
)

const liveDecisionsPerHourLimit = 60

func (c *Container) initDecisionModel() {
	model, err := typesafe.New(typesafe.Config{
		APIKey:      c.cfg.OpenRouterAPIKey,
		Model:       c.cfg.DecisionModel,
		HTTPReferer: c.cfg.OpenRouterHTTPReferer,
		XTitle:      c.cfg.OpenRouterXTitle,
	}, aibilling.NewPublisher(c.services.billingQueuePub))
	if err != nil {
		log.Fatalf("decision model: %v", err)
	}
	c.services.decisionModel = model
}

func (c *Container) initLiveDecisions() {
	reads := livedecision_repository.NewReadStore(c.db)
	service := livedecisions_usecase.NewService(livedecisions_usecase.Deps{
		Subjects: c.services.liveSubjects,
		Model:    c.services.decisionModel,
		Policy:   decision.DefaultPolicy(),
		Snapshots: livedecisions_usecase.ConversationSnapshots{
			Messages:  c.repositories.conversation,
			Stages:    c.repositories.stage,
			Campaigns: c.services.campaignWorkspaceResolver,
		},
		Reads:   reads,
		Log:     livedecision_repository.NewLog(c.db),
		Stages:  livedecisions_usecase.StageEffects{Assign: c.useCases.assignEntryStage, Hub: c.services.conversationHub},
		Live:    c.services.conversationHub,
		Funds:   balance_usecase.NewRequiredSpendGuard(c.services.cachedBalanceChecker, "live decisions"),
		Limiter: livedecisions_usecase.HourlyLimiter{State: c.redisProvider.SharedState(), Max: liveDecisionsPerHourLimit},
	})
	c.useCases.liveDecisions = service

	coalescer, err := livedecisions_usecase.NewCoalescer(c.redisProvider.SharedState(), service.DecideLater, livedecisions_usecase.CoalescerConfig{})
	if err != nil {
		log.Fatalf("live decisions: %v", err)
	}
	c.liveCoalescer = coalescer
	c.services.liveGate.Bind(livedecisions_usecase.NewGate(service, coalescer))

	if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
		SetLiveGate(conversation_usecase.LiveMode)
	}); ok {
		setter.SetLiveGate(c.services.liveGate)
	}
	if setter, ok := c.services.inboxService.(interface {
		SetLiveReadProvider(conversation_domain.LiveReadProvider)
	}); ok {
		setter.SetLiveReadProvider(livedecisions_usecase.InboxReads{Reads: reads})
	}
}

func (c *Container) startLiveDecisions() {
	if c.liveCoalescer == nil {
		return
	}
	var ctx context.Context
	ctx, c.liveCoalescerCancel = context.WithCancel(context.Background())
	go c.liveCoalescer.Run(ctx)
}
