package affiliate_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
)

func strPtr(s string) *string   { return &s }
func f64Ptr(f float64) *float64 { return &f }
func boolPtr(b bool) *bool      { return &b }

func TestUpdateMyAffiliate_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewUpdateMyAffiliateUseCase(repo, nil, nil)
	res, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{
		UserID:        "user-1",
		BrandName:     strPtr("New Brand"),
		BrandLogoURL:  strPtr("https://cdn/new.png"),
		AsaasWalletID: strPtr("new-wallet"),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.BrandName != "New Brand" || res.AsaasWalletID != "new-wallet" {
		t.Fatalf("not updated: %+v", res)
	}
}

func TestUpdateMyAffiliate_EmptyUser(t *testing.T) {
	uc := NewUpdateMyAffiliateUseCase(newMockRepo(), nil, nil)
	if _, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{UserID: " "}); !errors.Is(err, affiliate.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestUpdateMyAffiliate_NotFound(t *testing.T) {
	uc := NewUpdateMyAffiliateUseCase(newMockRepo(), nil, nil)
	if _, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{UserID: "x"}); !errors.Is(err, affiliate.ErrAffiliateNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestUpdateMyAffiliate_ValidateFails(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewUpdateMyAffiliateUseCase(repo, nil, nil)
	_, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{
		UserID:       "user-1",
		BrandLogoURL: strPtr("not-a-url"),
	})
	if !errors.Is(err, affiliate.ErrInvalidBrandLogoURL) {
		t.Fatalf("want ErrInvalidBrandLogoURL, got %v", err)
	}
}

func TestUpdateMyAffiliate_UpdateRepoError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.failUpdate = errors.New("db")
	uc := NewUpdateMyAffiliateUseCase(repo, nil, nil)
	_, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{UserID: "user-1"})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestAdminUpdateAffiliate_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewAdminUpdateAffiliateUseCase(repo)
	res, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{
		ID:            "aff-1",
		CommissionPct: f64Ptr(0.08),
		Active:        boolPtr(false),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.CommissionPct != 0.08 || res.Active {
		t.Fatalf("not updated: %+v", res)
	}
}

func TestAdminUpdateAffiliate_NotFound(t *testing.T) {
	uc := NewAdminUpdateAffiliateUseCase(newMockRepo())
	if _, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{ID: "x"}); !errors.Is(err, affiliate.ErrAffiliateNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestAdminUpdateAffiliate_InvalidPct(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewAdminUpdateAffiliateUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{
		ID: "aff-1", CommissionPct: f64Ptr(99.0),
	}); !errors.Is(err, affiliate.ErrInvalidCommissionPct) {
		t.Fatalf("want ErrInvalidCommissionPct, got %v", err)
	}
}

func TestAdminUpdateAffiliate_UpdateError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.failUpdate = errors.New("db")
	uc := NewAdminUpdateAffiliateUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{ID: "aff-1"}); err == nil {
		t.Fatal("want error")
	}
}

func tierPtr(t affiliate.Tier) *affiliate.Tier { return &t }

func TestAdminUpdateAffiliate_TierPromoteToReseller(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewAdminUpdateAffiliateUseCase(repo)
	res, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{
		ID:   "aff-1",
		Tier: tierPtr(affiliate.TierReseller),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Tier != affiliate.TierReseller {
		t.Fatalf("tier=%q", res.Tier)
	}

	stored, _ := repo.GetByID(context.Background(), "aff-1")
	if stored.Tier != affiliate.TierReseller {
		t.Fatalf("not persisted: tier=%q", stored.Tier)
	}
}

func TestAdminUpdateAffiliate_TierDemoteToAffiliate(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].Tier = affiliate.TierReseller
	uc := NewAdminUpdateAffiliateUseCase(repo)
	res, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{
		ID:   "aff-1",
		Tier: tierPtr(affiliate.TierAffiliate),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Tier != affiliate.TierAffiliate {
		t.Fatalf("tier=%q", res.Tier)
	}
}

func TestAdminUpdateAffiliate_TierInvalidRejected(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewAdminUpdateAffiliateUseCase(repo)
	bad := affiliate.Tier("wholesaler")
	_, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{
		ID:   "aff-1",
		Tier: &bad,
	})
	if !errors.Is(err, affiliate.ErrInvalidTier) {
		t.Fatalf("want ErrInvalidTier, got %v", err)
	}

	stored, _ := repo.GetByID(context.Background(), "aff-1")
	if stored.Tier == affiliate.Tier("wholesaler") {
		t.Fatalf("invalid tier leaked into storage")
	}
}

func TestAdminUpdateAffiliate_TierNilLeavesUnchanged(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].Tier = affiliate.TierReseller
	uc := NewAdminUpdateAffiliateUseCase(repo)
	res, err := uc.Execute(context.Background(), affiliate.AdminUpdateAffiliateInput{
		ID:     "aff-1",
		Active: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Tier != affiliate.TierReseller {
		t.Fatalf("tier was mutated unexpectedly: %q", res.Tier)
	}
}

func TestAdminListAffiliates(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	seedAffiliate(repo, "aff-2", "user-2")
	uc := NewAdminListAffiliatesUseCase(repo)
	list, total, err := uc.Execute(context.Background(), 1, 10)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("unexpected: %v %d %v", err, total, list)
	}
}

func TestAdminGetAffiliate_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewAdminGetAffiliateUseCase(repo, NewGetAffiliateStatsUseCase(repo))
	res, err := uc.Execute(context.Background(), "aff-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Affiliate.ID != "aff-1" || res.Stats == nil {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestAdminGetAffiliate_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewAdminGetAffiliateUseCase(repo, NewGetAffiliateStatsUseCase(repo))
	if _, err := uc.Execute(context.Background(), "x"); !errors.Is(err, affiliate.ErrAffiliateNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestAdminGetAffiliate_StatsError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.failCountRef = errors.New("db")
	uc := NewAdminGetAffiliateUseCase(repo, NewGetAffiliateStatsUseCase(repo))
	if _, err := uc.Execute(context.Background(), "aff-1"); err == nil {
		t.Fatal("want error")
	}
}
