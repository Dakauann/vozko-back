package container

import (
	"context"
	"log"
	"time"

	h "vozko/domain/emailhygiene"
	"vozko/infra/dnslookup"
	"vozko/infra/emailhygiene/disposable"
	"vozko/infra/emailwiring"
	"vozko/usecases/shared/delayedfire"
)

const emailHygieneBootTimeout = 30 * time.Second

type emailHygieneBundle struct {
	*emailwiring.Hygiene
	Checks *delayedfire.Consumer
}

func (c *Container) emailHygiene() *emailHygieneBundle {
	if c.emailHygieneBundle == nil {
		c.emailHygieneBundle = c.buildEmailHygiene()
	}
	return c.emailHygieneBundle
}

func (c *Container) buildEmailHygiene() *emailHygieneBundle {
	if c.db == nil || c.services == nil || c.services.webhookQueuePub == nil || c.services.webhookQueueSub == nil {
		log.Fatalf("[email-hygiene] the database and the webhook queue must exist first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), emailHygieneBootTimeout)
	defer cancel()
	built, err := emailwiring.NewHygiene(ctx, c.db, emailwiring.HygieneAdapters{
		Resolver: dnslookup.NewMailDomains(nil, h.LookupTimeout),
		Source:   disposable.NewSource(nil, disposable.UpstreamURL),
		QueuePub: c.services.webhookQueuePub,
	})
	if err != nil {
		log.Fatalf("[email-hygiene] %v", err)
	}
	checks, err := built.NewDomainCheckConsumer(c.services.webhookQueueSub)
	if err != nil {
		log.Fatalf("[email-hygiene] domain check consumer: %v", err)
	}
	return &emailHygieneBundle{Hygiene: built, Checks: checks}
}

func (c *Container) startEmailHygiene(b *emailHygieneBundle) {
	if b == nil || b.Checks == nil {
		log.Fatalf("[email-hygiene] the domain check consumer was not built")
	}
	if err := b.Checks.Start(); err != nil {
		log.Fatalf("[email-hygiene] failed to start the domain check consumer: %v", err)
	}
	b.KeepDisposableFresh(context.Background())
	log.Printf("[email-hygiene] domain checks and the disposable list (%d domains) are ready", b.Disposable.Set().Len())
}
