package analytics_usecase

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	analytics_domain "vozko/domain/analytics"
)

const invoiceCheckParallelism = 4

type getMetaInvoiceCheckUseCase struct {
	repo analytics_domain.MetaInvoiceCheckRepository
	meta analytics_domain.MetaPricingAnalytics
}

func NewGetMetaInvoiceCheckUseCase(repo analytics_domain.MetaInvoiceCheckRepository, meta analytics_domain.MetaPricingAnalytics) analytics_domain.GetMetaInvoiceCheckUseCase {
	return &getMetaInvoiceCheckUseCase{repo: repo, meta: meta}
}

func (uc *getMetaInvoiceCheckUseCase) Execute(ctx context.Context, start, end time.Time) (*analytics_domain.MetaInvoiceCheckReport, error) {
	start, end = normalizeCostPeriod(start, end)
	all, err := uc.repo.InvoiceAccounts(start, end)
	if err != nil {
		return nil, err
	}
	accounts, idle := analytics_domain.SplitActive(all)

	checks := make([]analytics_domain.WABAInvoiceCheck, len(accounts))
	slots := make(chan struct{}, invoiceCheckParallelism)
	var wg sync.WaitGroup
	for i, account := range accounts {
		if reason, unreadable := account.Unreadable(); unreadable {
			checks[i] = analytics_domain.UnavailableInvoice(account, reason)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			checks[i] = uc.check(ctx, account, start, end)
		}()
	}
	wg.Wait()

	sort.SliceStable(checks, func(a, b int) bool { return invoiceRank(checks[a]) < invoiceRank(checks[b]) })
	totals := analytics_domain.SumInvoice(checks)
	totals.IdleAccounts = idle
	return &analytics_domain.MetaInvoiceCheckReport{
		Period:   analytics_domain.Period{StartDate: start, EndDate: end},
		Totals:   totals,
		Accounts: checks,
	}, nil
}

func (uc *getMetaInvoiceCheckUseCase) check(ctx context.Context, account analytics_domain.InvoiceAccount, start, end time.Time) analytics_domain.WABAInvoiceCheck {
	points, err := uc.meta.Volumes(ctx, account.WABAID, account.AccessToken, start, end)
	if err != nil {
		log.Printf("[meta-invoice-check] pricing analytics of WABA %s could not be read: %v", account.WABAID, err)
		return analytics_domain.UnavailableInvoice(account, analytics_domain.ReasonMetaUnreadable)
	}
	return analytics_domain.CompareInvoice(account, analytics_domain.SumMetaVolumes(points))
}

func invoiceRank(check analytics_domain.WABAInvoiceCheck) int {
	switch check.State {
	case analytics_domain.InvoiceToCheck:
		return 0
	case analytics_domain.InvoiceMatched:
		return 1
	}
	return 2
}
