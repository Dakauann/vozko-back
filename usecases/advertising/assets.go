package advertising

import (
	"context"
	"strings"

	ads "vozko/domain/advertising"
)

type PromotablePage struct {
	Page    ads.RemotePage
	Numbers []ads.WorkspaceNumber
}

type assetsGateway interface {
	minimumGateway
	ListPages(ctx context.Context, token string) ([]ads.RemotePage, error)
	SearchLocations(ctx context.Context, token, query string) ([]ads.RemoteLocation, error)
	SearchTargeting(ctx context.Context, token, metaAccountID string, kind ads.TargetingSearchKind, query string) ([]ads.TargetingOption, error)
	EstimateReach(ctx context.Context, token, metaAccountID string, t ads.Targeting, p ads.Placements, goal ads.OptimizationGoal) (*ads.ReachEstimate, error)
	ListCatalogs(ctx context.Context, token, businessID string) ([]ads.RemoteCatalog, error)
	ListApps(ctx context.Context, token, metaAccountID string) ([]ads.RemoteApp, error)
	ListPagePosts(ctx context.Context, token, pageID string) ([]ads.RemotePost, error)
	ListInstagramMedia(ctx context.Context, token, instagramUserID string) ([]ads.RemotePost, error)
	ListInstantExperiences(ctx context.Context, token, pageID string) ([]ads.RemoteInstantExperience, error)
	ListPixels(ctx context.Context, token, metaAccountID string) ([]ads.Pixel, error)
	CreatePixel(ctx context.Context, token, metaAccountID, name string) (string, error)
}

type AssetsUseCase struct {
	access  accountAccess
	gateway assetsGateway
	numbers ads.NumberDirectory
}

func NewAssetsUseCase(sync *SyncUseCase, gateway assetsGateway, numbers ads.NumberDirectory) *AssetsUseCase {
	return &AssetsUseCase{access: sync.access, gateway: gateway, numbers: numbers}
}

func remote[T any](uc *AssetsUseCase, ctx context.Context, workspaceID, accountID string, use ads.AccountUse, call func(*ads.AdAccount, string) (T, error)) (T, error) {
	var zero T
	account, token, err := uc.access.open(ctx, workspaceID, accountID, use)
	if err != nil {
		return zero, err
	}
	out, err := call(account, token)
	if err != nil {
		return zero, uc.access.failed(ctx, account, err)
	}
	return out, nil
}

func (uc *AssetsUseCase) Pages(ctx context.Context, workspaceID, accountID string) ([]PromotablePage, error) {
	pages, err := remote(uc, ctx, workspaceID, accountID, ads.UseRead, func(_ *ads.AdAccount, token string) ([]ads.RemotePage, error) {
		return uc.gateway.ListPages(ctx, token)
	})
	if err != nil {
		return nil, err
	}
	numbers, err := uc.numbers.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]PromotablePage, 0, len(pages))
	for _, p := range pages {
		out = append(out, PromotablePage{Page: p, Numbers: ads.NumbersLinkedTo(p, numbers)})
	}
	return out, nil
}

const minSearchRunes = 2

func searchable(query string) bool { return len([]rune(strings.TrimSpace(query))) >= minSearchRunes }

func (uc *AssetsUseCase) Locations(ctx context.Context, workspaceID, accountID, query string) ([]ads.RemoteLocation, error) {
	if !searchable(query) {
		return []ads.RemoteLocation{}, nil
	}
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(_ *ads.AdAccount, token string) ([]ads.RemoteLocation, error) {
		return uc.gateway.SearchLocations(ctx, token, strings.TrimSpace(query))
	})
}

func (uc *AssetsUseCase) Targeting(ctx context.Context, workspaceID, accountID string, kind ads.TargetingSearchKind, query string) ([]ads.TargetingOption, error) {
	switch kind {
	case ads.SearchInterests, ads.SearchBehaviors, ads.SearchLanguages:
	default:
		return nil, ads.FieldError("kind", "invalid")
	}
	if kind != ads.SearchBehaviors && !searchable(query) {
		return []ads.TargetingOption{}, nil
	}
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(account *ads.AdAccount, token string) ([]ads.TargetingOption, error) {
		return uc.gateway.SearchTargeting(ctx, token, account.MetaAccountID, kind, strings.TrimSpace(query))
	})
}

func (uc *AssetsUseCase) Reach(ctx context.Context, workspaceID, accountID string, t ads.Targeting, p ads.Placements, goal ads.OptimizationGoal) (*ads.ReachEstimate, error) {
	t.Normalize()
	if len(t.Locations) == 0 {
		return &ads.ReachEstimate{}, nil
	}
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(account *ads.AdAccount, token string) (*ads.ReachEstimate, error) {
		return uc.gateway.EstimateReach(ctx, token, account.MetaAccountID, t, p, goal)
	})
}

func (uc *AssetsUseCase) Catalogs(ctx context.Context, workspaceID, accountID string) ([]ads.RemoteCatalog, error) {
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(account *ads.AdAccount, token string) ([]ads.RemoteCatalog, error) {
		if account.BusinessID == "" {
			return []ads.RemoteCatalog{}, nil
		}
		return uc.gateway.ListCatalogs(ctx, token, account.BusinessID)
	})
}

func (uc *AssetsUseCase) Apps(ctx context.Context, workspaceID, accountID string) ([]ads.RemoteApp, error) {
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(account *ads.AdAccount, token string) ([]ads.RemoteApp, error) {
		return uc.gateway.ListApps(ctx, token, account.MetaAccountID)
	})
}

func (uc *AssetsUseCase) pageOfAccount(ctx context.Context, token, pageID string) (*ads.RemotePage, error) {
	pages, err := uc.gateway.ListPages(ctx, token)
	if err != nil {
		return nil, err
	}
	for i := range pages {
		if pages[i].PageID == pageID {
			return &pages[i], nil
		}
	}
	return nil, ads.FieldError("pageId", "not_available")
}

func (uc *AssetsUseCase) Posts(ctx context.Context, workspaceID, accountID, pageID, platform string) ([]ads.RemotePost, error) {
	if !ads.ValidPostPlatform(platform) {
		return nil, ads.FieldError("platform", "invalid")
	}
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(_ *ads.AdAccount, token string) ([]ads.RemotePost, error) {
		page, err := uc.pageOfAccount(ctx, token, pageID)
		if err != nil {
			return nil, err
		}
		if platform == ads.PlatformInstagram {
			if page.InstagramUserID == "" {
				return []ads.RemotePost{}, nil
			}
			return uc.gateway.ListInstagramMedia(ctx, token, page.InstagramUserID)
		}
		return uc.gateway.ListPagePosts(ctx, token, page.PageID)
	})
}

func (uc *AssetsUseCase) InstantExperiences(ctx context.Context, workspaceID, accountID, pageID string) ([]ads.RemoteInstantExperience, error) {
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(_ *ads.AdAccount, token string) ([]ads.RemoteInstantExperience, error) {
		if _, err := uc.pageOfAccount(ctx, token, pageID); err != nil {
			return nil, err
		}
		return uc.gateway.ListInstantExperiences(ctx, token, pageID)
	})
}

func (uc *AssetsUseCase) Pixels(ctx context.Context, workspaceID, accountID string) ([]ads.Pixel, error) {
	return remote(uc, ctx, workspaceID, accountID, ads.UseRead, func(account *ads.AdAccount, token string) ([]ads.Pixel, error) {
		return uc.gateway.ListPixels(ctx, token, account.MetaAccountID)
	})
}

const maxPixelNameRunes = 100

func (uc *AssetsUseCase) CreatePixel(ctx context.Context, workspaceID, accountID, name string) (*ads.Pixel, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxPixelNameRunes {
		return nil, ads.FieldError("name", "invalid")
	}
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(account *ads.AdAccount, token string) (*ads.Pixel, error) {
		id, err := uc.gateway.CreatePixel(ctx, token, account.MetaAccountID, name)
		if err != nil {
			return nil, err
		}
		return &ads.Pixel{MetaID: id, Name: name}, nil
	})
}

func (uc *AssetsUseCase) BudgetMinimum(ctx context.Context, workspaceID, accountID string, goal ads.OptimizationGoal, bidAmount int64) (ads.BudgetMinimum, error) {
	return remote(uc, ctx, workspaceID, accountID, ads.UseWrite, func(account *ads.AdAccount, token string) (ads.BudgetMinimum, error) {
		minimums, err := uc.gateway.MinimumBudgets(ctx, token, account.MetaAccountID, bidAmount)
		if err != nil {
			return ads.BudgetMinimum{}, err
		}
		return minimums.For(goal), nil
	})
}
