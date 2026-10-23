package container

import (
	"context"
	"log"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"

	emailsendinghttp "vozko/delivery/http/emailsending"
	"vozko/delivery/http/emailwebhooks"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/infra/alerting"
	"vozko/infra/dnslookup"
	"vozko/infra/emailwiring"
	"vozko/infra/postmaster"
	"vozko/infra/route53dns"
	"vozko/infra/ses"
	esuc "vozko/usecases/emailsending"
	"vozko/usecases/shared/delayedfire"
)

const emailClientsTimeout = 30 * time.Second

type emailSendingBundle struct {
	Module   *esuc.Module
	Raw      es.RawMessageStore
	Codec    *emailtoken.Codec
	Intake   *esuc.NotificationIntake
	Consumer *esuc.Consumer
	Setup    *delayedfire.Consumer
	Handler  *emailsendinghttp.Handler
	Admin    *emailsendinghttp.AdminHandler
	Webhooks *emailwebhooks.Handler
}

func (c *Container) buildEmailSending() *emailSendingBundle {
	cfg := c.cfg.Email
	if c.db == nil || c.repositories == nil || c.repositories.workspace == nil {
		log.Fatalf("[email] the database and the workspace repository must exist before email sending")
	}
	if c.useCases == nil || c.useCases.publishWebhook == nil {
		log.Fatalf("[email] the webhook publisher must exist before email sending")
	}
	if c.redisProvider == nil || c.services == nil || c.services.webhookQueueSub == nil || c.services.webhookQueuePub == nil {
		log.Fatalf("[email] redis and the webhook queue must exist before email sending")
	}

	ops := alerting.NewLogOpsAlerter()
	shared := c.redisProvider.SharedState()
	managementCalls, err := ses.NewManagementCalls(shared, ops, ses.DefaultManagementCallsConfig())
	if err != nil {
		log.Fatalf("[email] SES management call limiter: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), emailClientsTimeout)
	defer cancel()
	clients, err := ses.NewClients(ctx, ses.Credentials{
		Region: cfg.AWSRegion, AccessKeyID: cfg.AWSAccessKeyID, SecretAccessKey: cfg.AWSSecretAccessKey, EndpointURL: cfg.AWSEndpointURL,
	}, managementCalls)
	if err != nil {
		log.Fatalf("[email] aws clients: %v", err)
	}
	provider, err := ses.New(clients.SES, ses.Config{
		Region:         cfg.AWSRegion,
		AccountID:      cfg.AWSAccountID,
		EventsTopicARN: cfg.EventsTopicARN,
	})
	if err != nil {
		log.Fatalf("[email] provider: %v", err)
	}
	raw, err := ses.NewRawMessageStore(clients.Objects, cfg.InboundBucket)
	if err != nil {
		log.Fatalf("[email] inbound store: %v", err)
	}
	dns, err := c.emailSharedDomainDNS(shared, ops, clients.Config)
	if err != nil {
		log.Fatalf("[email] dns: %v", err)
	}
	monitor, err := postmaster.New(postmaster.Config{
		ClientID: c.cfg.Google.ClientID, ClientSecret: c.cfg.Google.ClientSecret, RefreshToken: cfg.PostmasterRefreshToken,
	})
	if err != nil {
		log.Fatalf("[email] deliverability monitor: %v", err)
	}
	codec, err := emailtoken.NewCodec(cfg.TokenSecret)
	if err != nil {
		log.Fatalf("[email] EMAIL_TOKEN_SECRET: %v", err)
	}
	if cfg.AWSEndpointURL != "" {
		log.Printf("[email] provider endpoint overridden (aws=%q); unset it in production", cfg.AWSEndpointURL)
	}

	module, err := emailwiring.NewSendingModule(c.db, emailwiring.SendingAdapters{
		Provider: provider,
		DNS:      dns,
		DMARC:    dnslookup.New(nil),
		Records:  dnslookup.NewRecords(nil),
		Monitor:  monitor,
		Decoder:  ses.Decoder{},
		Alerts:   c.notifications().Publisher,
		Ops:      ops,
		Tenants:  c.emailTenantLifecycle(provider, clients.SES),
		Routes:   c.emailInboundRoutes(clients.Receipts, cfg.InboundTopicARN, cfg.InboundBucket),
		Quota:    ses.NewAccountQuota(clients.SES),
		Shared:   shared,
		QueuePub: c.services.webhookQueuePub,
		Codec:    codec,
		Hygiene:  c.emailHygiene().Service,
		Plans:    c.useCases.ensureActiveWorkspaceSubscription,
		Config: esuc.Config{
			ProbationDomain: cfg.SharedProbationDomain,
			TrustedDomain:   cfg.SharedTrustedDomain,
			PublicBaseURL:   cfg.PublicBaseURL,
		},
	})
	if err != nil {
		log.Fatalf("[email] use cases: %v", err)
	}
	intake, err := esuc.NewNotificationIntake(c.useCases.publishWebhook)
	if err != nil {
		log.Fatalf("[email] notification intake: %v", err)
	}
	consumer, err := module.NewConsumer(esuc.ConsumerDeps{
		QueueSub:    c.services.webhookQueueSub,
		QueuePub:    c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(),
		Durable:     c.processedWebhookEvents(),
	})
	if err != nil {
		log.Fatalf("[email] consumer: %v", err)
	}
	setupConsumer, err := module.NewSetupConsumer(c.services.webhookQueueSub)
	if err != nil {
		log.Fatalf("[email] setup consumer: %v", err)
	}
	verifier, err := emailwebhooks.NewVerifier(cfg.AWSRegion, emailwebhooks.NewHTTPCertificateFetcher(nil))
	if err != nil {
		log.Fatalf("[email] sns verifier: %v", err)
	}
	return &emailSendingBundle{
		Module:   module,
		Raw:      raw,
		Codec:    codec,
		Intake:   intake,
		Consumer: consumer,
		Setup:    setupConsumer,
		Handler:  emailsendinghttp.NewHandler(module.Setup, module.Domains, module.Senders, module.Suppressions),
		Admin:    emailsendinghttp.NewAdminHandler(module.Admin),
		Webhooks: emailwebhooks.NewHandler(emailwebhooks.Deps{
			Verifier:      verifier,
			AllowedTopics: cfg.AllowedTopicARNs,
			Intake:        intake,
			Confirmer:     emailwebhooks.NewHTTPConfirmer(nil),
		}),
	}
}

func (c *Container) emailSharedDomainDNS(shared ses.LimiterState, ops ses.OpsAlerter, aws awssdk.Config) (*route53dns.DNSRecords, error) {
	cfg := c.cfg.Email
	calls, err := ses.NewManagementCalls(shared, ops, ses.ManagementCallsConfig{
		Spacing:      route53dns.CallSpacing,
		Service:      route53dns.ServiceName,
		SlotKey:      route53dns.CallSlotKey,
		WantedPrefix: route53dns.CallWantedPrefix,
	})
	if err != nil {
		return nil, err
	}
	client, err := route53dns.NewClient(aws, cfg.AWSEndpointURL, ses.PaceCalls(calls, route53dns.EveryCall))
	if err != nil {
		return nil, err
	}
	return route53dns.New(client, route53dns.Config{Zones: map[string]string{
		cfg.SharedProbationDomain: cfg.Route53ProbationZoneID,
		cfg.SharedTrustedDomain:   cfg.Route53TrustedZoneID,
	}})
}

func (c *Container) startEmailSendingConsumer(b *emailSendingBundle) {
	if b == nil || b.Consumer == nil || b.Setup == nil {
		log.Fatalf("[email] the provider notification and setup consumers were not built")
	}
	if err := b.Consumer.Start(); err != nil {
		log.Fatalf("[email] failed to start the provider notification consumer: %v", err)
	}
	if err := b.Setup.Start(); err != nil {
		log.Fatalf("[email] failed to start the setup consumer: %v", err)
	}
	log.Printf("[email] provider notification consumer started (shared domains %s)", strings.Join([]string{c.cfg.Email.SharedProbationDomain, c.cfg.Email.SharedTrustedDomain}, ", "))
}
