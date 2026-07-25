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
	workspace_plan_usecase "vozko/usecases/workspace_plan"
)

func New() *Container {
	c := &Container{}
	c.cfg = config.LoadConfig()

	// Refuse to start on an unsafe port plan (RTP inside the OS ephemeral range, SIP/RTP
	// overlap, odd RTP start, a listener inside the RTP window) before anything binds.
	c.validatePortLayout()

	c.replicaID = c.cfg.ReplicaID
	log.Printf("Replica ID: %s", c.replicaID)
	c.redisProvider = redisCache.NewRedisProvider(c.cfg.RedisAddr, c.cfg.RedisPassword)
	c.initDatabase()
	c.initPII()
	c.initServices()
	c.initRepositories()

	c.seedPricingDefaults()
	c.initSIPTrunkManager()
	c.services.whatsappClientFactory = template_infra.NewWhatsAppClientFactory(c.repositories.businessPhone, &http.Client{Timeout: 30 * time.Second}, c.cfg.Dialog360MessagingBase, c.cfg.WhatsAppAppID)

	currentSubscriptionUC := workspace_plan_usecase.NewEnsureCurrentWorkspaceSubscriptionUseCase(c.repositories.workspaceSubscription)
	activeSubscriptionUC := workspace_plan_usecase.NewEnsureActiveWorkspaceSubscriptionUseCase(currentSubscriptionUC)
	planPricingAdapter := workspace_plan_usecase.NewPlanPricingAdapter(c.repositories.workspaceSubscription, c.repositories.workspacePlan)
	whatsappPricer := workspace_pricing.NewPricer(c.repositories.workspacePricing, workspace_pricing.WithPlanPricingProvider(planPricingAdapter))
	consumeWhatsappTemplateUC := balance_usecase.NewConsumeWhatsappTemplateUseCase(c.repositories.balance, whatsappPricer, activeSubscriptionUC)

	c.wireConversationHub(consumeWhatsappTemplateUC)
	c.initDialerTransferStack()
	c.initBranchRegistrar()
	c.agentMCP = c.initAgentMCP()
	c.initUseCases(consumeWhatsappTemplateUC)
	c.startConversationHub()
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

	if c.dialerTransferReaperCancel != nil {
		c.dialerTransferReaperCancel()
	}

	c.metricsHTTP.Shutdown()

	// Drain the branch registrar BEFORE the trunk manager: it BYEs active branch legs and
	// CANCELs in-flight rings, and those bridges ride on trunk media sessions, so it must
	// finish before the trunk manager closes them out from under a live call.
	if c.services.branchRegistrar != nil {
		c.services.branchRegistrar.Stop()
		log.Println("branch registrar drained")
	}

	if c.services.sipTrunkManager != nil {
		if err := c.services.sipTrunkManager.Stop(); err != nil {
			log.Printf("SIP trunk manager shutdown error: %v", err)
		}
	}

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
