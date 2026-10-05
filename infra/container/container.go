package container

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"vozko/domain/cluster"
	"vozko/domain/workspace/workspace_pricing"
	redisCache "vozko/infra/cache"
	"vozko/infra/cloudflare"
	"vozko/infra/config"
	template_infra "vozko/infra/whatsapp/template"

	balance_usecase "vozko/usecases/balance"
	conversation_usecase "vozko/usecases/conversation"
	livedecisions_usecase "vozko/usecases/livedecisions"
	"vozko/usecases/whatsapp/servicemessage"

	conversation_repository "vozko/infra/repositories/conversation"
	workspace_plan_usecase "vozko/usecases/workspace_plan"
)

func New() *Container {
	c := &Container{}
	c.cfg = config.LoadConfig()

	c.validatePortLayout()

	c.replicaID = c.cfg.ReplicaID
	log.Printf("Replica ID: %s", c.replicaID)
	c.redisProvider = redisCache.NewRedisProvider(c.cfg.RedisAddr, c.cfg.RedisPassword)
	c.initDatabase()
	c.initPII()
	c.initServices()
	c.initDecisionModel()
	c.initRepositories()
	c.wireContainerPipelines()

	c.seedPricingDefaults()
	c.services.whatsappClientFactory = template_infra.NewWhatsAppClientFactory(c.repositories.businessPhone, &http.Client{Timeout: 30 * time.Second}, c.cfg.Dialog360MessagingBase, c.cfg.WhatsAppAppID)

	currentSubscriptionUC := workspace_plan_usecase.NewEnsureCurrentWorkspaceSubscriptionUseCase(c.repositories.workspaceSubscription)
	activeSubscriptionUC := workspace_plan_usecase.NewEnsureActiveWorkspaceSubscriptionUseCase(currentSubscriptionUC)
	planPricingAdapter := workspace_plan_usecase.NewPlanPricingAdapter(c.repositories.workspaceSubscription, c.repositories.workspacePlan)
	whatsappPricer := workspace_pricing.NewPricer(c.repositories.workspacePricing, workspace_pricing.WithPlanPricingProvider(planPricingAdapter))
	consumeWhatsappTemplateUC := balance_usecase.NewConsumeWhatsappTemplateUseCase(c.repositories.balance, whatsappPricer, activeSubscriptionUC, c.repositories.monthlySendSlots)

	c.services.cachedBalanceChecker = balance_usecase.NewCachedBalanceChecker(
		c.repositories.balance, c.redisProvider.SharedState(), 10*time.Second)
	c.services.liveGate = &livedecisions_usecase.GateHandle{}
	c.services.liveSubjects = conversation_usecase.NewLiveSubjects()
	c.services.analysisScheduler = conversation_usecase.NewAnalysisScheduler(c.redisProvider.SharedState(), c.services.liveGate)

	serviceMessageBilling, err := servicemessage.NewBilling(servicemessage.Deps{
		Pricer:         whatsappPricer,
		Ledger:         c.repositories.balance,
		BalanceChecker: c.services.cachedBalanceChecker,
		Unattributed:   conversation_repository.NewUnattributedServiceMessageRepository(c.db),
	})
	if err != nil {
		log.Fatalf("Failed to build the WhatsApp service message billing: %v", err)
	}

	c.services.serviceMessageBilling = serviceMessageBilling

	c.initSIPTrunks()
	c.wireConversationHub()
	c.initCallSessionRegistries()
	c.agentMCP = c.initAgentMCP()
	c.initInstagram()
	c.initFacebook()
	c.initTelegram()
	c.initWebchat()
	c.initUnofficialWhatsApp()
	c.initUseCases(consumeWhatsappTemplateUC)
	c.syncLinkedRoles()
	c.initAds()
	c.initImageGeneration()
	c.initLiveDecisions()
	c.startConversationHub()
	c.wireInstagramConversationStack()
	c.wireFacebookConversationStack()
	c.wireTelegramConversationStack()
	c.wireWebchatConversationStack()
	c.wireUnofficialWhatsAppConversationStack()
	c.wireDelegations()
	c.initHandlers()
	c.initRouter()
	c.initServer()
	c.initMetricsServer()
	c.initJobRunner()
	return c
}

func (c *Container) Start(port string) error {
	log.Printf("Starting server on port %s...", port)

	c.jobRunner.StartAll()
	log.Println("Background jobs started")
	c.startLiveDecisions()
	c.startSIPTrunks()

	c.metricsHTTP.Start()

	c.cfPublisher = cloudflare.New(
		c.cfg.CFAccountID,
		c.cfg.CFKVNamespace,
		c.cfg.CFKVAPIToken,
		c.cfg.CFKVKeyPrefix,
		c.replicaID,
		c.cfg.PublicReplicaURL,
		"",
	)
	if c.cfPublisher == nil {
		log.Println("[cf-kv] publisher disabled (REPLICA_PUBLIC_URL empty)")
	} else {
		var pubCtx context.Context
		pubCtx, c.cfPublisherCancel = context.WithCancel(context.Background())
		go c.cfPublisher.Run(pubCtx)
	}

	return c.server.Start(port)
}

func (c *Container) Shutdown() {
	log.Println("shutting down HTTP server...")
	if err := c.server.Shutdown(); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	if c.cfPublisherCancel != nil {
		c.cfPublisherCancel()
	}
	if c.liveCoalescerCancel != nil {
		c.liveCoalescerCancel()
	}

	c.stopSIPTrunks()

	c.metricsHTTP.Shutdown()

	if c.services.clusterRegistry != nil && c.replicaID != "" {
		if err := c.services.clusterRegistry.Withdraw(c.replicaID); err != nil {
			log.Printf("cluster registry withdraw failed: %v", err)
		}
		_ = c.redisProvider.SharedState().Del(cluster.HeartbeatKeyPrefix + c.replicaID)
	}

	if c.recordingPool != nil {
		c.recordingPool.Shutdown()
		log.Println("recording pool shut down")
	}
}

func (c *Container) ResetConversationHistory() error {
	if c == nil || c.repositories == nil || c.repositories.conversation == nil {
		return fmt.Errorf("conversation repository not initialized")
	}

	if err := c.repositories.conversation.ClearAll(); err != nil {
		return err
	}

	log.Println("Conversation history cleared")
	return nil
}

func (c *Container) seedPricingDefaults() {
	if err := c.repositories.workspacePricing.SeedDefaults(workspace_pricing.DefaultPricingCatalog); err != nil {
		log.Printf("WARNING: failed to seed default pricing items: %v", err)
	}
}
