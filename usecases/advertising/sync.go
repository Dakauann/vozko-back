package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	ads "vozko/domain/advertising"
)

const (
	RecentInsightDays  = 3
	SettledInsightDays = 28
	syncBatchSize      = 100
)

var structureLevels = []ads.Level{ads.LevelCampaign, ads.LevelAdSet, ads.LevelAd}

type syncGateway interface {
	GetAdAccount(ctx context.Context, token, metaAccountID string) (*ads.RemoteAdAccount, error)
	ListObjects(ctx context.Context, token, metaAccountID string, level ads.Level) ([]*ads.Object, error)
	DailyInsights(ctx context.Context, token, metaAccountID string, r ads.DateRange) ([]ads.DailyInsight, error)
}

type SyncUseCase struct {
	access   accountAccess
	gateway  syncGateway
	objects  ads.ObjectRepository
	insights ads.InsightRepository
}

func NewSyncUseCase(accounts ads.AccountRepository, grants ads.GrantRepository, gateway syncGateway, objects ads.ObjectRepository, insights ads.InsightRepository) *SyncUseCase {
	return &SyncUseCase{
		access:   accountAccess{accounts: accounts, grants: grants, now: func() time.Time { return time.Now().UTC() }},
		gateway:  gateway,
		objects:  objects,
		insights: insights,
	}
}

func (uc *SyncUseCase) Sync(ctx context.Context, workspaceID, accountID string) (*ads.AdAccount, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.ScopeAdsRead)
	if err != nil {
		return nil, err
	}
	if err := uc.syncAccount(ctx, account, token, RecentInsightDays); err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	return account, nil
}

func (uc *SyncUseCase) SyncAll(ctx context.Context, insightDays int) error {
	var failures []error
	for offset := 0; ; offset += syncBatchSize {
		accounts, err := uc.access.accounts.ListConnected(ctx, syncBatchSize, offset)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if err := uc.syncConnected(ctx, account, insightDays); err != nil {
				log.Printf("[ads] sync of ad account %s failed: %v", account.ID, err)
				failures = append(failures, err)
			}
		}
		if len(accounts) < syncBatchSize {
			break
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("ads: %d ad account(s) failed to sync: %w", len(failures), errors.Join(failures...))
	}
	return nil
}

func (uc *SyncUseCase) syncConnected(ctx context.Context, account *ads.AdAccount, insightDays int) error {
	token, err := uc.access.tokenFor(ctx, account, ads.ScopeAdsRead)
	if err != nil {
		return err
	}
	return uc.access.failed(ctx, account, uc.syncAccount(ctx, account, token, insightDays))
}

func (uc *SyncUseCase) syncAccount(ctx context.Context, account *ads.AdAccount, token string, insightDays int) error {
	remote, err := uc.gateway.GetAdAccount(ctx, token, account.MetaAccountID)
	if err != nil {
		return err
	}
	applyRemote(account, *remote)
	if err := uc.access.accounts.Upsert(ctx, account); err != nil {
		return err
	}
	if err := uc.SyncStructure(ctx, account, token); err != nil {
		return err
	}
	if err := uc.syncInsights(ctx, account, token, insightDays); err != nil {
		return err
	}
	now := uc.access.now()
	account.LastSyncedAt = &now
	return uc.access.accounts.MarkSynced(ctx, account.ID, now)
}

func (uc *SyncUseCase) SyncStructure(ctx context.Context, account *ads.AdAccount, token string) error {
	now := uc.access.now()
	for _, level := range structureLevels {
		objects, err := uc.gateway.ListObjects(ctx, token, account.MetaAccountID, level)
		if err != nil {
			return fmt.Errorf("ads: list %s objects: %w", level, err)
		}
		for _, o := range objects {
			o.WorkspaceID = account.WorkspaceID
			o.AdAccountID = account.ID
			o.Level = level
			o.SyncedAt = now
		}
		if err := uc.objects.ReplaceLevel(ctx, account.ID, level, objects, now); err != nil {
			return err
		}
	}
	return nil
}

func (uc *SyncUseCase) syncInsights(ctx context.Context, account *ads.AdAccount, token string, days int) error {
	loc, err := account.Location()
	if err != nil {
		return err
	}
	currency, err := ads.NormalizeCurrency(account.Currency)
	if err != nil {
		return err
	}
	now := uc.access.now()
	r := ads.LastDays(days, now, loc)
	rows, err := uc.gateway.DailyInsights(ctx, token, account.MetaAccountID, r)
	if err != nil {
		return fmt.Errorf("ads: read insights: %w", err)
	}
	for i := range rows {
		if rows[i].Currency != currency {
			return fmt.Errorf("%w: insight in %q for an account in %s", ads.ErrMixedCurrencies, rows[i].Currency, currency)
		}
		rows[i].AdAccountID = account.ID
		rows[i].FetchedAt = now
	}
	return uc.insights.ReplaceDays(ctx, account.ID, r, rows)
}
