package affiliate_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/affiliate"
)

func seedAffiliate(repo *mockRepo, id, userID string) *affiliate.Affiliate {
	a := &affiliate.Affiliate{
		ID:            id,
		UserID:        userID,
		Code:          affiliate.NormalizeCode("CODE-" + id),
		BrandName:     "Brand",
		BrandLogoURL:  "https://cdn/x.png",
		AsaasWalletID: "w",
		CommissionPct: 0.05,
		Active:        true,
	}
	cpy := *a
	repo.affiliates[id] = &cpy
	return a
}

func TestGetAffiliateStats(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")

	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1", ReferredAt: time.Now()}
	repo.referrals["ws-2"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-2", ReferredAt: time.Now()}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	repo.earnings["inv-1"] = &affiliate.Earning{AffiliateID: "aff-1", InvoiceID: "inv-1", AmountMicros: 1000, CreatedAt: monthStart.Add(-24 * time.Hour)}
	repo.earnings["inv-2"] = &affiliate.Earning{AffiliateID: "aff-1", InvoiceID: "inv-2", AmountMicros: 500, CreatedAt: monthStart.Add(time.Hour)}

	uc := NewGetAffiliateStatsUseCase(repo)
	stats, err := uc.Execute(context.Background(), "aff-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stats.TotalReferrals != 2 {
		t.Fatalf("total referrals: %d", stats.TotalReferrals)
	}
	if stats.TotalEarningMicros != 1500 {
		t.Fatalf("total earnings: %d", stats.TotalEarningMicros)
	}
	if stats.MonthEarningMicros != 500 {
		t.Fatalf("month earnings: %d", stats.MonthEarningMicros)
	}
}

func TestGetAffiliateStats_CountError(t *testing.T) {
	repo := newMockRepo()
	repo.failCountRef = errors.New("db")
	uc := NewGetAffiliateStatsUseCase(repo)
	if _, err := uc.Execute(context.Background(), "aff-1"); err == nil {
		t.Fatal("want error")
	}
}

func TestGetAffiliateStats_SumAllError(t *testing.T) {
	repo := newMockRepo()
	repo.failSumAll = errors.New("db")
	uc := NewGetAffiliateStatsUseCase(repo)
	if _, err := uc.Execute(context.Background(), "aff-1"); err == nil {
		t.Fatal("want error")
	}
}

func TestGetAffiliateStats_SumSinceError(t *testing.T) {
	repo := newMockRepo()
	repo.failSumSince = errors.New("db")
	uc := NewGetAffiliateStatsUseCase(repo)
	if _, err := uc.Execute(context.Background(), "aff-1"); err == nil {
		t.Fatal("want error")
	}
}

func TestGetMyAffiliate_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	stats := NewGetAffiliateStatsUseCase(repo)
	uc := NewGetMyAffiliateUseCase(repo, stats)
	res, err := uc.Execute(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Affiliate.ID != "aff-1" || res.Stats == nil {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestGetMyAffiliate_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetMyAffiliateUseCase(repo, NewGetAffiliateStatsUseCase(repo))
	_, err := uc.Execute(context.Background(), "nobody")
	if !errors.Is(err, affiliate.ErrAffiliateNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestGetMyAffiliate_StatsError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.failCountRef = errors.New("db")
	uc := NewGetMyAffiliateUseCase(repo, NewGetAffiliateStatsUseCase(repo))
	if _, err := uc.Execute(context.Background(), "user-1"); err == nil {
		t.Fatal("want error")
	}
}

func TestListReferrals(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewListReferralsUseCase(repo)
	list, total, err := uc.Execute(context.Background(), "user-1", 1, 10)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("unexpected: %v %d %v", err, total, list)
	}
}

func TestListReferrals_UserNotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewListReferralsUseCase(repo)
	if _, _, err := uc.Execute(context.Background(), "u", 1, 10); err == nil {
		t.Fatal("want error")
	}
}

func TestListEarnings(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.earnings["inv-1"] = &affiliate.Earning{AffiliateID: "aff-1", InvoiceID: "inv-1"}
	uc := NewListEarningsUseCase(repo)
	list, total, err := uc.Execute(context.Background(), "user-1", 1, 10)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("unexpected: %v %d %v", err, total, list)
	}
}

func TestListEarnings_UserNotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewListEarningsUseCase(repo)
	if _, _, err := uc.Execute(context.Background(), "u", 1, 10); err == nil {
		t.Fatal("want error")
	}
}
