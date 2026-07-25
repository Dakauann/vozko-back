package affiliate_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/affiliate"
	config_domain "vozko/domain/config"
)

func buildRegisterInput() affiliate.RegisterAffiliateInput {
	return affiliate.RegisterAffiliateInput{
		UserID:        "user-1",
		BrandName:     "Cool Brand",
		BrandLogoURL:  "https://cdn.example.com/logo.png",
		AsaasWalletID: "wallet-123",
	}
}

func TestRegisterAffiliate_Success(t *testing.T) {
	repo := newMockRepo()
	users := newMockUserRepo("user-1")
	sys := &mockSystemConfigRepo{cfg: &config_domain.SystemConfig{AffiliateCommissionPct: 0.07}}
	uc := NewRegisterAffiliateUseCase(repo, users, sys, nil)

	in := buildRegisterInput()
	aff, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aff == nil || aff.Code != "COOL-BRAND" {
		t.Fatalf("unexpected affiliate: %+v", aff)
	}
	if aff.CommissionPct != 0.07 {
		t.Fatalf("expected pct from system config, got %v", aff.CommissionPct)
	}
	if !aff.Active {
		t.Fatalf("should be active")
	}
}

func TestRegisterAffiliate_EmptyUser(t *testing.T) {
	uc := NewRegisterAffiliateUseCase(newMockRepo(), newMockUserRepo(), nil, nil)
	_, err := uc.Execute(context.Background(), affiliate.RegisterAffiliateInput{UserID: " "})
	if !errors.Is(err, affiliate.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestRegisterAffiliate_UserNotFound(t *testing.T) {
	uc := NewRegisterAffiliateUseCase(newMockRepo(), newMockUserRepo(), nil, nil)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if !errors.Is(err, affiliate.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestRegisterAffiliate_AlreadyExists(t *testing.T) {
	repo := newMockRepo()
	repo.affiliates["existing"] = &affiliate.Affiliate{ID: "existing", UserID: "user-1"}
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if !errors.Is(err, affiliate.ErrAffiliateAlreadyExists) {
		t.Fatalf("want ErrAffiliateAlreadyExists, got %v", err)
	}
}

func TestRegisterAffiliate_RepoGetByUserError(t *testing.T) {
	repo := newMockRepo()
	repo.failGetByUser = errors.New("db error")
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if err == nil || err.Error() != "db error" {
		t.Fatalf("want db error, got %v", err)
	}
}

func TestRegisterAffiliate_InvalidCode_BrandAlsoInvalid(t *testing.T) {
	uc := NewRegisterAffiliateUseCase(newMockRepo(), newMockUserRepo("user-1"), nil, nil)
	in := buildRegisterInput()
	in.BrandName = "!!!"
	in.Code = ""
	_, err := uc.Execute(context.Background(), in)
	if !errors.Is(err, affiliate.ErrInvalidCode) {
		t.Fatalf("want ErrInvalidCode, got %v", err)
	}
}

func TestRegisterAffiliate_SystemConfigErrorFallsBackToDefault(t *testing.T) {
	repo := newMockRepo()
	sys := &mockSystemConfigRepo{err: errors.New("boom")}
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), sys, nil)
	aff, err := uc.Execute(context.Background(), buildRegisterInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aff.CommissionPct != affiliate.DefaultCommissionPct {
		t.Fatalf("expected default pct, got %v", aff.CommissionPct)
	}
}

func TestRegisterAffiliate_CodeCollision_RetriesWithSuffix(t *testing.T) {
	repo := newMockRepo()
	repo.affiliates["aff-a"] = &affiliate.Affiliate{ID: "aff-a", UserID: "other", Code: "COOL-BRAND"}
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	aff, err := uc.Execute(context.Background(), buildRegisterInput())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if aff.Code != "COOL-BRAND-2" {
		t.Fatalf("want suffix code, got %q", aff.Code)
	}
}

func TestRegisterAffiliate_CodeCollision_SuffixProducesInvalidCode(t *testing.T) {

	repo := newMockRepo()

	maxBrand := strings.Repeat("A", 32)
	repo.affiliates["existing"] = &affiliate.Affiliate{ID: "existing", UserID: "other", Code: maxBrand}

	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	in := buildRegisterInput()
	in.Code = maxBrand
	_, err := uc.Execute(context.Background(), in)
	if !errors.Is(err, affiliate.ErrInvalidCode) {
		t.Fatalf("want ErrInvalidCode (suffix overflow), got %v", err)
	}
}

func TestRegisterAffiliate_CodeCollision_Exhausted(t *testing.T) {
	repo := newMockRepo()

	for i := 0; i < 11; i++ {
		code := "COOL-BRAND"
		if i > 0 {
			code = code + "-" + itoa(i+1)
		}
		repo.affiliates["aff-"+itoa(i)] = &affiliate.Affiliate{ID: "aff-" + itoa(i), UserID: "other-" + itoa(i), Code: code}
	}
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if !errors.Is(err, affiliate.ErrCodeAlreadyTaken) {
		t.Fatalf("want ErrCodeAlreadyTaken, got %v", err)
	}
}

func TestRegisterAffiliate_GetByCodeError(t *testing.T) {
	repo := newMockRepo()
	repo.failGetByCode = errors.New("db")
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if err == nil {
		t.Fatalf("want error")
	}
}

func TestRegisterAffiliate_ValidateFailsOnBadInput(t *testing.T) {
	uc := NewRegisterAffiliateUseCase(newMockRepo(), newMockUserRepo("user-1"), nil, nil)
	in := buildRegisterInput()
	in.BrandLogoURL = "not-a-url"
	_, err := uc.Execute(context.Background(), in)
	if !errors.Is(err, affiliate.ErrInvalidBrandLogoURL) {
		t.Fatalf("want ErrInvalidBrandLogoURL, got %v", err)
	}
}

func TestRegisterAffiliate_RepoCreateError(t *testing.T) {
	repo := newMockRepo()
	repo.failCreate = errors.New("db create")
	uc := NewRegisterAffiliateUseCase(repo, newMockUserRepo("user-1"), nil, nil)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if err == nil || !strings.Contains(err.Error(), "failed to create affiliate") {
		t.Fatalf("want wrapped create error, got %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+(i%10))) + out
		i /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

var _ = time.Now
