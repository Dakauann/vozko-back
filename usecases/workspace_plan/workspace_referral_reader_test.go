package workspace_plan_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
)

type fakeAffiliateRepo struct {
	byUserID    map[string]*affiliate.Affiliate
	byUserIDErr error

	referrals     map[string]*affiliate.Referral
	referralErr   error
	referralCalls int
	getByUserHits int
}

func newFakeAffiliateRepo() *fakeAffiliateRepo {
	return &fakeAffiliateRepo{
		byUserID:  make(map[string]*affiliate.Affiliate),
		referrals: make(map[string]*affiliate.Referral),
	}
}

func (f *fakeAffiliateRepo) Create(ctx context.Context, a *affiliate.Affiliate) error {
	return nil
}
func (f *fakeAffiliateRepo) GetByID(ctx context.Context, id string) (*affiliate.Affiliate, error) {
	return nil, affiliate.ErrAffiliateNotFound
}
func (f *fakeAffiliateRepo) GetByUserID(ctx context.Context, userID string) (*affiliate.Affiliate, error) {
	f.getByUserHits++
	if f.byUserIDErr != nil {
		return nil, f.byUserIDErr
	}
	a, ok := f.byUserID[userID]
	if !ok {
		return nil, affiliate.ErrAffiliateNotFound
	}
	return a, nil
}
func (f *fakeAffiliateRepo) GetByCode(ctx context.Context, code string) (*affiliate.Affiliate, error) {
	return nil, affiliate.ErrAffiliateNotFound
}
func (f *fakeAffiliateRepo) List(ctx context.Context, page, pageSize int) ([]affiliate.Affiliate, int64, error) {
	return nil, 0, nil
}
func (f *fakeAffiliateRepo) Update(ctx context.Context, a *affiliate.Affiliate) error { return nil }
func (f *fakeAffiliateRepo) CreateReferral(ctx context.Context, r *affiliate.Referral) error {
	return nil
}
func (f *fakeAffiliateRepo) GetReferralByWorkspaceID(ctx context.Context, workspaceID string) (*affiliate.Referral, error) {
	f.referralCalls++
	if f.referralErr != nil {
		return nil, f.referralErr
	}
	r, ok := f.referrals[workspaceID]
	if !ok {
		return nil, affiliate.ErrAffiliateNotFound
	}
	return r, nil
}
func (f *fakeAffiliateRepo) ListReferralsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]affiliate.Referral, int64, error) {
	return nil, 0, nil
}
func (f *fakeAffiliateRepo) CountReferralsByAffiliateID(ctx context.Context, affiliateID string) (int64, error) {
	return 0, nil
}
func (f *fakeAffiliateRepo) CreateEarning(ctx context.Context, e *affiliate.Earning) error {
	return nil
}
func (f *fakeAffiliateRepo) GetEarningByInvoiceID(ctx context.Context, invoiceID string) (*affiliate.Earning, error) {
	return nil, affiliate.ErrAffiliateNotFound
}
func (f *fakeAffiliateRepo) ListEarningsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]affiliate.Earning, int64, error) {
	return nil, 0, nil
}
func (f *fakeAffiliateRepo) SumEarningsByAffiliateID(ctx context.Context, affiliateID string) (int64, error) {
	return 0, nil
}
func (f *fakeAffiliateRepo) SumEarningsSince(ctx context.Context, affiliateID string, sinceUnix int64) (int64, error) {
	return 0, nil
}

type fakeOwnerReader struct {
	owners map[string]string
	err    error
	calls  int
}

func newFakeOwnerReader() *fakeOwnerReader {
	return &fakeOwnerReader{owners: make(map[string]string)}
}

func (f *fakeOwnerReader) GetOwnerUserID(workspaceID string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.owners[workspaceID], nil
}

func TestWorkspaceReferralReader_NilSafety(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var a *workspaceReferralReaderAdapter
		got, err := a.GetAffiliateIDByWorkspaceID("ws-1")
		if err != nil || got != "" {
			t.Fatalf("nil receiver: got=%q err=%v", got, err)
		}
	})
	t.Run("nil affiliates repo", func(t *testing.T) {
		r := NewWorkspaceReferralReader(nil, newFakeOwnerReader())
		got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
		if err != nil || got != "" {
			t.Fatalf("nil repo: got=%q err=%v", got, err)
		}
	})
	t.Run("empty workspace id", func(t *testing.T) {
		repo := newFakeAffiliateRepo()
		r := NewWorkspaceReferralReader(repo, newFakeOwnerReader())
		got, err := r.GetAffiliateIDByWorkspaceID("")
		if err != nil || got != "" {
			t.Fatalf("empty ws id: got=%q err=%v", got, err)
		}
		if repo.referralCalls != 0 || repo.getByUserHits != 0 {
			t.Fatalf("expected no repo calls, got referral=%d byUser=%d",
				repo.referralCalls, repo.getByUserHits)
		}
	})
}

func TestWorkspaceReferralReader_ReferralWins(t *testing.T) {
	repo := newFakeAffiliateRepo()
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-real-referral"}

	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "owner-user"
	repo.byUserID["owner-user"] = &affiliate.Affiliate{
		ID: "aff-owner-reseller", Active: true, Tier: affiliate.TierReseller,
	}

	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "aff-real-referral" {
		t.Fatalf("want referral id, got %q", got)
	}
	if owners.calls != 0 {
		t.Fatalf("owner reader must not be consulted when referral exists, calls=%d", owners.calls)
	}
}

func TestWorkspaceReferralReader_ReferralRepoError(t *testing.T) {
	repo := newFakeAffiliateRepo()
	boom := errors.New("db down")
	repo.referralErr = boom
	r := NewWorkspaceReferralReader(repo, newFakeOwnerReader())
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if !errors.Is(err, boom) {
		t.Fatalf("want db error to propagate, got err=%v got=%q", err, got)
	}
	if got != "" {
		t.Fatalf("want empty id on error, got %q", got)
	}
}

func TestWorkspaceReferralReader_FallbackDisabledWhenNoOwnerReader(t *testing.T) {
	repo := newFakeAffiliateRepo()
	r := NewWorkspaceReferralReader(repo, nil)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil || got != "" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if repo.getByUserHits != 0 {
		t.Fatalf("owner-affiliate lookup must not run when reader is nil, hits=%d", repo.getByUserHits)
	}
}

func TestWorkspaceReferralReader_OwnerHasNoAffiliate(t *testing.T) {
	repo := newFakeAffiliateRepo()
	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "owner-user"
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil || got != "" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestWorkspaceReferralReader_OwnerIsRegularAffiliateNoFallback(t *testing.T) {
	repo := newFakeAffiliateRepo()
	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "owner-user"
	repo.byUserID["owner-user"] = &affiliate.Affiliate{
		ID: "aff-owner", Active: true, Tier: affiliate.TierAffiliate,
	}
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil || got != "" {
		t.Fatalf("regular affiliate must NOT trigger fallback; got=%q err=%v", got, err)
	}
}

func TestWorkspaceReferralReader_OwnerIsInactiveResellerNoFallback(t *testing.T) {
	repo := newFakeAffiliateRepo()
	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "owner-user"
	repo.byUserID["owner-user"] = &affiliate.Affiliate{
		ID: "aff-owner", Active: false, Tier: affiliate.TierReseller,
	}
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil || got != "" {
		t.Fatalf("inactive reseller must NOT trigger fallback; got=%q err=%v", got, err)
	}
}

func TestWorkspaceReferralReader_OwnerIsActiveResellerFallback(t *testing.T) {
	repo := newFakeAffiliateRepo()
	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "reseller-user"
	repo.byUserID["reseller-user"] = &affiliate.Affiliate{
		ID: "aff-reseller", Active: true, Tier: affiliate.TierReseller,
	}
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "aff-reseller" {
		t.Fatalf("want fallback to reseller aff id, got %q", got)
	}
}

func TestWorkspaceReferralReader_OwnerReaderError(t *testing.T) {
	repo := newFakeAffiliateRepo()
	owners := newFakeOwnerReader()
	boom := errors.New("workspace repo down")
	owners.err = boom
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if !errors.Is(err, boom) {
		t.Fatalf("want owner reader error to propagate, got err=%v got=%q", err, got)
	}
}

func TestWorkspaceReferralReader_OwnerEmpty(t *testing.T) {
	repo := newFakeAffiliateRepo()
	owners := newFakeOwnerReader()
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil || got != "" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if repo.getByUserHits != 0 {
		t.Fatalf("should not look up affiliate for empty owner id, hits=%d", repo.getByUserHits)
	}
}

func TestWorkspaceReferralReader_GetByUserIDOtherErrorPropagates(t *testing.T) {
	repo := newFakeAffiliateRepo()
	boom := errors.New("affiliate repo down")
	repo.byUserIDErr = boom
	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "owner-user"
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if !errors.Is(err, boom) {
		t.Fatalf("want affiliate lookup error to propagate, got err=%v got=%q", err, got)
	}
}

func TestWorkspaceReferralReader_ReferralWithEmptyAffiliateIDFallsThrough(t *testing.T) {

	repo := newFakeAffiliateRepo()
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: ""}
	owners := newFakeOwnerReader()
	owners.owners["ws-1"] = "reseller-user"
	repo.byUserID["reseller-user"] = &affiliate.Affiliate{
		ID: "aff-reseller", Active: true, Tier: affiliate.TierReseller,
	}
	r := NewWorkspaceReferralReader(repo, owners)
	got, err := r.GetAffiliateIDByWorkspaceID("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "aff-reseller" {
		t.Fatalf("empty AffiliateID on referral should fall through to fallback, got %q", got)
	}
}
