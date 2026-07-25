package affiliate_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
)

func TestValidateReferralCode_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewValidateReferralCodeUseCase(repo)
	res, err := uc.Execute(context.Background(), "code-aff-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Valid || res.Code != "CODE-AFF-1" {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestValidateReferralCode_EmptyOrBad(t *testing.T) {
	uc := NewValidateReferralCodeUseCase(newMockRepo())
	res, err := uc.Execute(context.Background(), "")
	if err != nil || res.Valid {
		t.Fatalf("want invalid, got %v %+v", err, res)
	}
}

func TestValidateReferralCode_NotFound(t *testing.T) {
	uc := NewValidateReferralCodeUseCase(newMockRepo())
	res, err := uc.Execute(context.Background(), "NOPE")
	if err != nil || res.Valid {
		t.Fatalf("want invalid, got %v %+v", err, res)
	}
}

func TestValidateReferralCode_Inactive(t *testing.T) {
	repo := newMockRepo()
	a := seedAffiliate(repo, "aff-1", "user-1")
	a.Active = false
	repo.affiliates["aff-1"].Active = false
	uc := NewValidateReferralCodeUseCase(repo)
	res, _ := uc.Execute(context.Background(), "code-aff-1")
	if res.Valid {
		t.Fatalf("should be invalid")
	}
}

func TestValidateReferralCode_RepoError(t *testing.T) {
	repo := newMockRepo()
	repo.failGetByCode = errors.New("db")
	uc := NewValidateReferralCodeUseCase(repo)
	if _, err := uc.Execute(context.Background(), "ABC"); err == nil {
		t.Fatal("want error")
	}
}

func TestTrackReferral_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewTrackReferralUseCase(repo)
	ref, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{
		Code: "code-aff-1", WorkspaceID: "ws-1", WorkspaceOwnerUserID: "user-2",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.AffiliateID != "aff-1" || ref.WorkspaceID != "ws-1" {
		t.Fatalf("unexpected: %+v", ref)
	}
}

func TestTrackReferral_EmptyWorkspace(t *testing.T) {
	uc := NewTrackReferralUseCase(newMockRepo())
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "X"}); !errors.Is(err, affiliate.ErrInvalidReferralCode) {
		t.Fatalf("want ErrInvalidReferralCode, got %v", err)
	}
}

func TestTrackReferral_EmptyCode(t *testing.T) {
	uc := NewTrackReferralUseCase(newMockRepo())
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{WorkspaceID: "ws", Code: ""}); !errors.Is(err, affiliate.ErrInvalidReferralCode) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestTrackReferral_CodeNotFound(t *testing.T) {
	uc := NewTrackReferralUseCase(newMockRepo())
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "NOPE", WorkspaceID: "ws"}); !errors.Is(err, affiliate.ErrInvalidReferralCode) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestTrackReferral_CodeLookupError(t *testing.T) {
	repo := newMockRepo()
	repo.failGetByCode = errors.New("db")
	uc := NewTrackReferralUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "ABC", WorkspaceID: "ws"}); err == nil {
		t.Fatal("want error")
	}
}

func TestTrackReferral_Inactive(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].Active = false
	uc := NewTrackReferralUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "code-aff-1", WorkspaceID: "ws"}); !errors.Is(err, affiliate.ErrInvalidReferralCode) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestTrackReferral_SelfReferral(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	uc := NewTrackReferralUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{
		Code: "code-aff-1", WorkspaceID: "ws", WorkspaceOwnerUserID: "user-1",
	}); !errors.Is(err, affiliate.ErrSelfReferral) {
		t.Fatalf("want self, got %v", err)
	}
}

func TestTrackReferral_AlreadyReferred(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewTrackReferralUseCase(repo)
	_, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "code-aff-1", WorkspaceID: "ws-1"})
	if !errors.Is(err, affiliate.ErrWorkspaceAlreadyReferred) {
		t.Fatalf("want already referred, got %v", err)
	}
}

func TestTrackReferral_ReferralLookupError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.failGetRef = errors.New("db")
	uc := NewTrackReferralUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "code-aff-1", WorkspaceID: "ws"}); err == nil {
		t.Fatal("want error")
	}
}

func TestTrackReferral_CreateError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.failCreateRef = errors.New("db")
	uc := NewTrackReferralUseCase(repo)
	if _, err := uc.Execute(context.Background(), affiliate.TrackReferralInput{Code: "code-aff-1", WorkspaceID: "ws"}); err == nil {
		t.Fatal("want error")
	}
}
