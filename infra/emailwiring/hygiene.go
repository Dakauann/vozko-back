package emailwiring

import (
	"context"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	h "vozko/domain/emailhygiene"
	"vozko/domain/messaging"
	"vozko/infra/emailhygiene/disposable"
	emailhygiene_repository "vozko/infra/repositories/emailhygiene"
	hyuc "vozko/usecases/emailhygiene"
	"vozko/usecases/shared/delayedfire"
)

const (
	domainCheckFireID      = "domain_checks"
	DisposableReloadPeriod = time.Hour
)

type HygieneAdapters struct {
	Resolver h.MailDomainResolver
	Source   h.DisposableSource
	QueuePub messaging.MessageQueuePub
}

type Hygiene struct {
	Service    *hyuc.Service
	Domains    *hyuc.Domains
	Disposable *hyuc.Disposable
	Pruner     *hyuc.Pruner
}

type domainCheckKick struct {
	scheduler *delayedfire.Scheduler
}

func (k domainCheckKick) Kick() error {
	return k.scheduler.ScheduleFire(domainCheckFireID, time.Now())
}

func NewReputation(db *gorm.DB) *hyuc.Reputation {
	return hyuc.NewReputation(hyuc.ReputationDeps{
		Bad:      emailhygiene_repository.NewBadAddresses(db),
		Activity: emailhygiene_repository.NewActivity(db),
	})
}

func NewHygiene(ctx context.Context, db *gorm.DB, a HygieneAdapters) (*Hygiene, error) {
	if db == nil || a.Resolver == nil || a.Source == nil || a.QueuePub == nil {
		return nil, fmt.Errorf("%w: the database, the resolver, the disposable list source and the queue publisher", ErrIncomplete)
	}
	scheduler, err := delayedfire.NewScheduler(a.QueuePub, h.DomainCheckTopic)
	if err != nil {
		return nil, fmt.Errorf("emailwiring: domain check kick: %w", err)
	}
	checks := emailhygiene_repository.NewDomainChecks(db)
	lists := emailhygiene_repository.NewDisposableLists(db)
	domains := hyuc.NewDomains(hyuc.DomainDeps{Resolver: a.Resolver, Checks: checks, Kick: domainCheckKick{scheduler: scheduler}})
	list, err := hyuc.NewDisposable(hyuc.DisposableDeps{Store: lists, Source: a.Source}, disposable.Snapshot())
	if err != nil {
		return nil, err
	}
	if err := list.Load(ctx); err != nil {
		log.Printf("[email-hygiene] the embedded disposable snapshot stays in use: %v", err)
	}
	return &Hygiene{
		Service:    hyuc.NewService(hyuc.ServiceDeps{Standing: emailhygiene_repository.NewStanding(db), Domains: domains, Disposable: list}),
		Domains:    domains,
		Disposable: list,
		Pruner:     hyuc.NewPruner(hyuc.PrunerDeps{Domains: checks, Activity: emailhygiene_repository.NewActivity(db), Lists: lists}),
	}, nil
}

func (b *Hygiene) NewDomainCheckConsumer(sub messaging.MessageQueueSub) (*delayedfire.Consumer, error) {
	return delayedfire.NewConsumer(sub, h.DomainCheckTopic, "email-domain-checks", func(ctx context.Context, _ string) error {
		return b.Domains.RunDue(ctx)
	})
}

func (b *Hygiene) KeepDisposableFresh(ctx context.Context) {
	ticker := time.NewTicker(DisposableReloadPeriod)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.Disposable.Reload(ctx)
			}
		}
	}()
}
