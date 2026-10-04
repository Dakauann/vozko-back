package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"

	ads "vozko/domain/advertising"
)

func (uc *SyncUseCase) WatchFunds(ctx context.Context) error {
	accounts, err := uc.accountsNeedingAttention(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, account := range accounts {
		if err := uc.watchFunds(ctx, account); err != nil {
			log.Printf("[ads] funds watch of ad account %s failed: %v", account.ID, err)
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("ads: %d ad account(s) failed the funds watch: %w", len(failures), errors.Join(failures...))
	}
	return nil
}

func (uc *SyncUseCase) accountsNeedingAttention(ctx context.Context) ([]*ads.AdAccount, error) {
	var out []*ads.AdAccount
	for offset := 0; ; offset += syncBatchSize {
		page, err := uc.access.accounts.ListByFundsLevels(ctx, ads.AttentionFundsLevels, syncBatchSize, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < syncBatchSize {
			return out, nil
		}
	}
}

func (uc *SyncUseCase) watchFunds(ctx context.Context, account *ads.AdAccount) error {
	token, err := uc.access.tokenFor(ctx, account, ads.UseRead)
	if err != nil {
		return err
	}
	if err := uc.refreshAccount(ctx, account, token); err != nil {
		return uc.access.failed(ctx, account, err)
	}
	return uc.recordFunds(ctx, account, token)
}

func (uc *SyncUseCase) refreshAccount(ctx context.Context, account *ads.AdAccount, token string) error {
	remote, err := uc.gateway.GetAdAccount(ctx, token, account.MetaAccountID)
	if err != nil {
		return err
	}
	applyRemote(account, *remote)
	return uc.access.accounts.Upsert(ctx, account)
}

func (uc *SyncUseCase) recordFunds(ctx context.Context, account *ads.AdAccount, token string) error {
	account.Billing = uc.billingKind(ctx, account, token)
	pace, err := uc.dailySpend(ctx, account)
	if err != nil {
		return err
	}
	account.DailySpendMicros = pace
	funds := ads.FundsOf(account)
	account.RecordFunds(funds.Level, uc.access.now())
	if err := uc.access.accounts.SaveFunds(ctx, account); err != nil {
		return err
	}
	if !funds.Level.NeedsAttention() {
		return nil
	}
	if err := uc.alerts.Alert(ctx, account, funds); err != nil {
		log.Printf("[ads] funds alert for ad account %s could not be sent (the next sync retries): %v", account.ID, err)
	}
	return nil
}

func (uc *SyncUseCase) billingKind(ctx context.Context, account *ads.AdAccount, token string) ads.BillingKind {
	if account.CanManage() != nil {
		return ads.BillingUnknown
	}
	billing, err := uc.gateway.GetBilling(ctx, token, account.MetaAccountID, false)
	if err != nil {
		log.Printf("[ads] billing of ad account %s could not be read, its funds stay unknown: %v", account.ID, err)
		return ads.BillingUnknown
	}
	return ads.BillingKindOf(billing.Prepay)
}

func (uc *SyncUseCase) dailySpend(ctx context.Context, account *ads.AdAccount) (int64, error) {
	loc, err := account.Location()
	if err != nil {
		return 0, err
	}
	yesterday := uc.access.now().In(loc).AddDate(0, 0, -1)
	rows, err := uc.insights.Rows(ctx, account.ID, ads.LastDays(ads.FundsPaceDays, yesterday, loc))
	if err != nil {
		return 0, err
	}
	return ads.AverageDailySpend(rows, ads.FundsPaceDays), nil
}
