package emailsending

import (
	"context"
	"fmt"
	"log"
	"time"

	es "vozko/domain/emailsending"
)

const deliverabilityPage = 100

type Deliverability struct {
	c *core
}

func (d *Deliverability) Execute(ctx context.Context) error {
	return d.Sweep(ctx, d.c.Now())
}

func (d *Deliverability) Sweep(ctx context.Context, now time.Time) error {
	c := d.c
	day := now.AddDate(0, 0, -1)
	failures := 0
	for _, shared := range []string{c.Config.ProbationDomain, c.Config.TrustedDomain} {
		stats, err := c.Monitor.SpamRate(ctx, shared, day)
		if err != nil {
			failures++
			log.Printf("[emailsending] spam rate of shared domain %s unavailable: %v", shared, err)
			continue
		}
		if stats.Alarming() {
			detail := fmt.Sprintf("shared domain %s had a %d per mil spam rate on %s", shared, stats.SpamRatePerMil, stats.Day)
			if err := c.Ops.Alert(ctx, "email: shared domain spam rate above the limit", detail); err != nil {
				log.Printf("[emailsending] ops alert failed: %v (%s)", err, detail)
			}
		}
	}
	for offset := 0; ; offset += deliverabilityPage {
		domains, err := c.Domains.ListVerified(ctx, es.DomainOwn, offset, deliverabilityPage)
		if err != nil {
			return err
		}
		for i := range domains {
			if err := d.checkOwn(ctx, &domains[i], day); err != nil {
				failures++
				log.Printf("[emailsending] spam rate of %s unavailable: %v", domains[i].Domain, err)
			}
		}
		if len(domains) < deliverabilityPage {
			break
		}
	}
	if failures > 0 {
		return fmt.Errorf("emailsending: %d spam rate checks failed", failures)
	}
	return nil
}

func (d *Deliverability) checkOwn(ctx context.Context, sd *es.SendingDomain, day time.Time) error {
	stats, err := d.c.Monitor.SpamRate(ctx, sd.Domain, day)
	if err != nil {
		return err
	}
	switch {
	case stats.Alarming():
		d.c.alertSpamRate(ctx, sd, stats)
	case stats.Available:
		d.c.resolve(ctx, sd.WorkspaceID, spamRateKey(sd.ID))
	}
	return nil
}
