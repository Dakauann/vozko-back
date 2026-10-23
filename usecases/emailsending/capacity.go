package emailsending

import (
	"context"
	"fmt"
	"log"

	es "vozko/domain/emailsending"
)

const capacityAccountsPage = 500

type CapacityCheck struct {
	c *core
}

func (k *CapacityCheck) Execute(ctx context.Context) error {
	c := k.c
	ctx = es.WithProviderLane(ctx, es.LaneBackground)
	accounts, err := c.allAccounts(ctx)
	if err != nil {
		return err
	}
	domains, err := c.ReplyPauses.ListAll(ctx)
	if err != nil {
		return err
	}
	filters, err := c.Routes.IPFilterCount(ctx)
	if err != nil {
		return providerFailure("ip filter count", err)
	}
	usages := es.ProviderUsages(es.InventoryOf(accounts, domains, filters))
	for _, u := range es.UsagesNearQuota(usages) {
		c.opsAlert(ctx, "Cota do SES perto do limite: "+string(u.Resource),
			fmt.Sprintf("%s: %d de %d (%s); peça o aumento à AWS antes de chegar ao limite", u.Resource, u.Used, u.Quota, basisPointsAsPercent(int64(u.BasisPoints()))))
	}
	for _, u := range usages {
		log.Printf("[emailsending] SES capacity %s: %d of %d", u.Resource, u.Used, u.Quota)
	}
	return nil
}

func (c *core) allAccounts(ctx context.Context) ([]es.SenderAccount, error) {
	var all []es.SenderAccount
	for offset := 0; ; offset += capacityAccountsPage {
		page, total, err := c.Accounts.List(ctx, offset, capacityAccountsPage)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < capacityAccountsPage || int64(len(all)) >= total {
			return all, nil
		}
	}
}
