package container

import (
	"log"
	"net/http"
	"time"

	recordings_domain "vozko/domain/calls/recordings"
	rag_domain "vozko/domain/rag"
	pricing_service "vozko/domain/services/pricing"
	"vozko/domain/shipping"
	shortlink_domain "vozko/domain/shortlink"
	rag_infra "vozko/infra/ai/rag"
	asaas_service "vozko/infra/asaas"
	whatsapp_client "vozko/infra/conversation/whatsapp"
	queue "vozko/infra/messaging"
	notification_service "vozko/infra/notifications"
	prometheus_service "vozko/infra/prometheus"
	"vozko/infra/s3"
	"vozko/infra/security"
	document_service "vozko/infra/services/document"
	inventory_service "vozko/infra/services/inventory"
	businessphone_infra "vozko/infra/whatsapp/business_phone"
	google_calendar "vozko/third_party/google_calendar"
)

func (c *Container) initServices() {

	passwordService := security.NewBcryptPasswordService(0)

	tokenIssuer, err := security.NewJWTTokenService(c.cfg.AuthJWTSecret, c.cfg.AuthJWTAccessTTL, c.cfg.AuthJWTRefreshTTL)
	if err != nil {
		log.Fatal("Failed to initialize token service:", err)
	}
	readMeTokenIssuer, err := security.NewJWTTokenService(c.cfg.AuthJWTSecret, c.cfg.ReadMeAuthTokenTTL, c.cfg.AuthJWTRefreshTTL)
	if err != nil {
		log.Fatal("Failed to initialize ReadMe token service:", err)
	}

	httpClient := &http.Client{
		Timeout: 120 * time.Second,
	}

	prometheusService := prometheus_service.NewPrometheusService(c.replicaID)

	inventorySvc := inventory_service.NewVariantStockService(c.db)
	documentValidator := document_service.NewValidator()

	storage := s3.NewS3Service()

	tmplLoader := notification_service.NewTemplateLoaderService("/infra/notifications/templates")
	emailSvc := notification_service.NewEmailService(tmplLoader, c.cfg.ResendAPIKey, c.cfg.ResendFromEmail, c.cfg.ResendFromName, c.cfg.ResendMaxRPS)

	shippingGateways := make(map[shipping.Provider]shipping.ProviderGateway)

	workflowWakeExchange := "workflow_wake_exchange"
	businessMetricsExchange := "business_metrics_exchange"
	crmTelemetryExchange := "crm_telemetry_exchange"
	notifications_exchange := "notifications_exchange"
	whatsappCampaignExchange := "whatsapp_campaign_exchange"
	ragDocProcessingExchange := rag_domain.DocumentProcessingExchange
	webhookExchange := "webhook_events_exchange"
	shortlinkClickExchange := shortlink_domain.ClickExchange
	callBillingExchange := "call_events_exchange"
	recordingExchange := recordings_domain.Exchange

	amqpPool := queue.NewConnectionPool(c.cfg.RabbitMQUsername, c.cfg.RabbitMQPassword, 0)

	crmTelPub := queue.NewRabbitMQQueuePub(amqpPool, crmTelemetryExchange)
	crmTelSub := queue.NewRabbitMQQueueSub(amqpPool, crmTelemetryExchange)

	c.services = &services{
		amqpPool:              amqpPool,
		workflowWakePub:       queue.NewRabbitMQQueuePub(amqpPool, workflowWakeExchange),
		workflowWakeSub:       queue.NewRabbitMQQueueSub(amqpPool, workflowWakeExchange),
		metricsQueuePub:       queue.NewRabbitMQQueuePub(amqpPool, businessMetricsExchange),
		metricsQueueSub:       queue.NewRabbitMQQueueSub(amqpPool, businessMetricsExchange),
		crmTelemetryPub:       crmTelPub,
		crmTelemetrySub:       crmTelSub,
		notificationsQueuePub: queue.NewRabbitMQQueuePub(amqpPool, notifications_exchange),
		notificationQueueSub:  queue.NewRabbitMQQueueSub(amqpPool, notifications_exchange),
		wcQueuePub:            queue.NewRabbitMQQueuePub(amqpPool, whatsappCampaignExchange),
		wcQueueSub:            queue.NewRabbitMQQueueSub(amqpPool, whatsappCampaignExchange),
		metrics:               prometheusService,
		cache:                 c.redisProvider.Cache(),
		rateLimiterFactory:    c.redisProvider.RateLimiterFactory(),
		password:              passwordService,
		tokenService:          tokenIssuer,
		readMeTokenService:    readMeTokenIssuer,
		fileStorage:           storage,
		ticketFileStorage:     storage,
		asaasService:          asaas_service.NewAsaasService(c.cfg.AsaasAPIKey, c.cfg.AsaasBaseURL),
		templateLoaderService: tmplLoader,
		emailService:          emailSvc,
		inventory:             inventorySvc,
		documentValidator:     documentValidator,
		pricingService:        pricing_service.NewPricingService(),
		shippingGateways:      shippingGateways,
		ragEmbedding:          rag_infra.NewEmbeddingService(c.cfg.OllamaURL, rag_domain.DefaultEmbeddingModel),
		ragTextChunker:        rag_infra.NewTextChunker(),
		ragTextExtractor:      rag_infra.NewTextExtractor(),
		ragQueuePub:           queue.NewRabbitMQQueuePub(amqpPool, ragDocProcessingExchange),
		ragQueueSub:           queue.NewRabbitMQQueueSub(amqpPool, ragDocProcessingExchange),
		shortlinkQueuePub:     queue.NewRabbitMQQueuePub(amqpPool, shortlinkClickExchange),
		shortlinkQueueSub:     queue.NewRabbitMQQueueSub(amqpPool, shortlinkClickExchange),
		webhookQueuePub:       queue.NewRabbitMQQueuePub(amqpPool, webhookExchange),
		webhookQueueSub:       queue.NewRabbitMQQueueSub(amqpPool, webhookExchange),
		billingQueuePub:       queue.NewRabbitMQQueuePub(amqpPool, callBillingExchange),
		billingQueueSub:       queue.NewRabbitMQQueueSub(amqpPool, callBillingExchange),
		recordingQueuePub:     queue.NewRabbitMQQueuePub(amqpPool, recordingExchange),
		recordingQueueSub:     queue.NewRabbitMQQueueSub(amqpPool, recordingExchange),
		googleCalendar:        google_calendar.NewService(c.cfg.GoogleOAuthClientID, c.cfg.GoogleOAuthClientSecret),
	}

	if c.cfg.WhatsAppAccessToken == "" || c.cfg.WhatsAppPhoneNumberID == "" {
		log.Println("WhatsApp client disabled: missing WHATSAPP_BUSINESS_PHONE_NUMBER_ID or WHATSAPP_ACCESS_TOKEN")
	} else {
		c.services.whatsapp = whatsapp_client.NewClient(whatsapp_client.Config{
			BaseURL:       c.cfg.WhatsAppAPIBaseURL,
			PhoneNumberID: c.cfg.WhatsAppPhoneNumberID,
			AccessToken:   c.cfg.WhatsAppAccessToken,
			AppID:         c.cfg.WhatsAppAppID,
			HTTPClient:    httpClient,
		})
	}

	c.services.businessPhoneMetaAPI = businessphone_infra.NewMetaAPIClient(httpClient)
	c.services.coexistenceMetaAPI = businessphone_infra.NewMetaCoexistenceClient(httpClient)
}
