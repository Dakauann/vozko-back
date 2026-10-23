package container

import (
	"log"
	"time"

	"vozko/brand"
	deliveryHttp "vozko/delivery/http"
	"vozko/delivery/http/apikeys"
	"vozko/delivery/http/emailapihttp"
	"vozko/domain/emailwebhook"
	cronPackage "vozko/infra/cron"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/emailwiring"
	"vozko/infra/netguard"
	apikey_repository "vozko/infra/repositories/apikey"
	apikey_usecase "vozko/usecases/apikey"
)

const (
	apiKeyRequestsPerMinute   = 600
	apiKeyIPRequestsPerMinute = 1200
)

type emailAPIBundle struct {
	*emailwiring.EmailAPI
	Handler *emailapihttp.Handler
}

func (c *Container) apiKeys() *apikey_usecase.Service {
	if c.apiKeyService != nil {
		return c.apiKeyService
	}
	if c.db == nil || c.redisProvider == nil {
		log.Fatalf("[api-keys] the database and redis must exist before the API keys")
	}
	svc, err := apikey_usecase.New(apikey_usecase.Deps{Keys: apikey_repository.New(c.db), Cache: c.redisProvider.SharedState()})
	if err != nil {
		log.Fatalf("[api-keys] %v", err)
	}
	c.apiKeyService = svc
	return svc
}

func (c *Container) apiKeyRoutes() deliveryHttp.APIKeyRoutes {
	if c.services == nil || c.services.rateLimiterFactory == nil {
		log.Fatalf("[api-keys] the rate limiter must exist before the key routes")
	}
	keys := c.apiKeys()
	return deliveryHttp.APIKeyRoutes{
		Handler: apikeys.NewHandler(keys),
		Auth: apikeys.NewAuthenticator(keys,
			c.services.rateLimiterFactory("api_key", apiKeyRequestsPerMinute, time.Minute),
			c.services.rateLimiterFactory("api_key_ip", apiKeyIPRequestsPerMinute, time.Minute)),
	}
}

func (c *Container) initEmailAPI() {
	email := c.email
	if email == nil || email.Sending == nil || email.Sending.Module == nil || email.Billing == nil {
		log.Fatalf("[email-api] email sending and billing must be built before the email API")
	}
	if c.services == nil || c.services.webhookQueuePub == nil || c.services.webhookQueueSub == nil || c.redisProvider == nil {
		log.Fatalf("[email-api] the webhook queue and redis must exist before the email API")
	}
	shared := c.redisProvider.SharedState()
	slots, ok := shared.(emailwiring.SlotState)
	if !ok {
		log.Fatalf("[email-api] the shared state cannot release owned keys, so webhook slots would leak")
	}
	cipher := piigorm.Service()
	if cipher == nil {
		log.Fatalf("[email-api] webhook secrets need the PII encryption service")
	}
	api, err := emailwiring.NewEmailAPI(c.db, emailwiring.EmailAPIAdapters{
		Sending:   email.Sending.Module,
		Billing:   email.Billing,
		Files:     mediaDownloads(),
		QueuePub:  c.services.webhookQueuePub,
		Shared:    slots,
		Alerts:    c.notifications().Publisher,
		Cipher:    cipher,
		Hosts:     netguard.NewHostChecker(nil),
		Client:    netguard.NewHTTPClient(emailwebhook.RequestTimeout),
		UserAgent: brand.Active().Name + " Webhooks",
	})
	if err != nil {
		log.Fatalf("[email-api] %v", err)
	}
	for _, consumer := range api.Consumers(c.services.webhookQueueSub, c.services.webhookQueuePub, shared) {
		if err := consumer.Start(); err != nil {
			log.Fatalf("[email-api] webhook consumer: %v", err)
		}
	}
	c.emailAPI = &emailAPIBundle{EmailAPI: api, Handler: emailapihttp.NewHandler(api.Service, api.Service, api.Webhooks)}
}

func (c *Container) registerEmailAPIJobs() {
	if c.emailAPI == nil {
		log.Fatalf("[email-api] jobs requested before the email API was built")
	}
	webhooks := c.emailAPI.Webhooks
	c.jobRunner.SetEmailWebhookJobs(cronPackage.CtxJobFunc(webhooks.Sweep), cronPackage.CtxJobFunc(webhooks.Purge))
}
