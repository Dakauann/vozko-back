package affiliate_usecase

import (
	"context"
	"time"

	"vozko/domain/affiliate"
)

type getAffiliateStatsUseCase struct {
	repo affiliate.Repository
}

func NewGetAffiliateStatsUseCase(repo affiliate.Repository) affiliate.GetAffiliateStatsUseCase {
	return &getAffiliateStatsUseCase{repo: repo}
}

func (uc *getAffiliateStatsUseCase) Execute(ctx context.Context, affiliateID string) (*affiliate.Stats, error) {
	total, err := uc.repo.CountReferralsByAffiliateID(ctx, affiliateID)
	if err != nil {
		return nil, err
	}
	earnedTotal, err := uc.repo.SumEarningsByAffiliateID(ctx, affiliateID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
	earnedMonth, err := uc.repo.SumEarningsSince(ctx, affiliateID, monthStart)
	if err != nil {
		return nil, err
	}
	return &affiliate.Stats{
		TotalReferrals:     total,
		TotalEarningMicros: earnedTotal,
		MonthEarningMicros: earnedMonth,
	}, nil
}

type getMyAffiliateUseCase struct {
	repo  affiliate.Repository
	stats affiliate.GetAffiliateStatsUseCase
}

func NewGetMyAffiliateUseCase(repo affiliate.Repository, stats affiliate.GetAffiliateStatsUseCase) affiliate.GetMyAffiliateUseCase {
	return &getMyAffiliateUseCase{repo: repo, stats: stats}
}

func (uc *getMyAffiliateUseCase) Execute(ctx context.Context, userID string) (*affiliate.AffiliateProfileWithStats, error) {
	aff, err := uc.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	s, err := uc.stats.Execute(ctx, aff.ID)
	if err != nil {
		return nil, err
	}
	return &affiliate.AffiliateProfileWithStats{Affiliate: aff, Stats: s}, nil
}

type listReferralsUseCase struct {
	repo affiliate.Repository
}

func NewListReferralsUseCase(repo affiliate.Repository) affiliate.ListReferralsUseCase {
	return &listReferralsUseCase{repo: repo}
}

func (uc *listReferralsUseCase) Execute(ctx context.Context, userID string, page, pageSize int) ([]affiliate.Referral, int64, error) {
	aff, err := uc.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return uc.repo.ListReferralsByAffiliateID(ctx, aff.ID, page, pageSize)
}

type listEarningsUseCase struct {
	repo affiliate.Repository
}

func NewListEarningsUseCase(repo affiliate.Repository) affiliate.ListEarningsUseCase {
	return &listEarningsUseCase{repo: repo}
}

func (uc *listEarningsUseCase) Execute(ctx context.Context, userID string, page, pageSize int) ([]affiliate.Earning, int64, error) {
	aff, err := uc.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return uc.repo.ListEarningsByAffiliateID(ctx, aff.ID, page, pageSize)
}
