package affiliate_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
)

func TestRegisterAffiliate_WalletValidator_Success(t *testing.T) {
	repo := newMockRepo()
	users := newMockUserRepo("user-1")
	wv := &mockWalletValidator{}
	uc := NewRegisterAffiliateUseCase(repo, users, nil, wv)

	aff, err := uc.Execute(context.Background(), buildRegisterInput())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if aff == nil {
		t.Fatal("nil affiliate")
	}
	if len(wv.calls) != 1 {
		t.Fatalf("expected 1 validator call, got %d", len(wv.calls))
	}
	call := wv.calls[0]
	if call.WalletID != aff.AsaasWalletID {
		t.Fatalf("walletID mismatch: %s vs %s", call.WalletID, aff.AsaasWalletID)
	}
	if call.CustomerName != "tester" || call.CustomerDoc != "11144477735" {
		t.Fatalf("expected identity propagated, got %+v", call)
	}
}

func TestRegisterAffiliate_WalletValidator_Rejects(t *testing.T) {
	repo := newMockRepo()
	users := newMockUserRepo("user-1")
	wv := &mockWalletValidator{err: affiliate.ErrInvalidAsaasWalletID}
	uc := NewRegisterAffiliateUseCase(repo, users, nil, wv)

	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if !errors.Is(err, affiliate.ErrInvalidAsaasWalletID) {
		t.Fatalf("want ErrInvalidAsaasWalletID, got %v", err)
	}

	if len(repo.affiliates) != 0 {
		t.Fatal("affiliate should not have been created")
	}
}

func TestRegisterAffiliate_WalletValidator_ProviderError(t *testing.T) {
	wv := &mockWalletValidator{err: affiliate.ErrWalletValidationFailed}
	uc := NewRegisterAffiliateUseCase(newMockRepo(), newMockUserRepo("user-1"), nil, wv)
	_, err := uc.Execute(context.Background(), buildRegisterInput())
	if !errors.Is(err, affiliate.ErrWalletValidationFailed) {
		t.Fatalf("want ErrWalletValidationFailed, got %v", err)
	}
}

func TestUpdateMyAffiliate_WalletValidator_SkippedWhenUnchanged(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	wv := &mockWalletValidator{}
	uc := NewUpdateMyAffiliateUseCase(repo, newMockUserRepo("user-1"), wv)

	newName := "New Brand Name"
	_, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{
		UserID:    "user-1",
		BrandName: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(wv.calls) != 0 {
		t.Fatalf("validator should not be called when wallet is unchanged, got %d calls", len(wv.calls))
	}
}

func TestUpdateMyAffiliate_WalletValidator_CalledOnChange(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	wv := &mockWalletValidator{}
	uc := NewUpdateMyAffiliateUseCase(repo, newMockUserRepo("user-1"), wv)

	newWallet := "new-wallet-xyz"
	_, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{
		UserID:        "user-1",
		AsaasWalletID: &newWallet,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(wv.calls) != 1 || wv.calls[0].WalletID != newWallet {
		t.Fatalf("validator should have been called with new wallet, got %+v", wv.calls)
	}
}

func TestUpdateMyAffiliate_WalletValidator_RejectsChange(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	wv := &mockWalletValidator{err: affiliate.ErrInvalidAsaasWalletID}
	uc := NewUpdateMyAffiliateUseCase(repo, newMockUserRepo("user-1"), wv)

	newWallet := "bad-wallet"
	_, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{
		UserID:        "user-1",
		AsaasWalletID: &newWallet,
	})
	if !errors.Is(err, affiliate.ErrInvalidAsaasWalletID) {
		t.Fatalf("want ErrInvalidAsaasWalletID, got %v", err)
	}

	got, _ := repo.GetByID(context.Background(), "aff-1")
	if got.AsaasWalletID == newWallet {
		t.Fatal("wallet change should have been rejected, but was persisted")
	}
}

func TestUpdateMyAffiliate_WalletValidator_MissingUserStillValidates(t *testing.T) {

	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	wv := &mockWalletValidator{}
	uc := NewUpdateMyAffiliateUseCase(repo, newMockUserRepo(), wv)

	newWallet := "w-new"
	if _, err := uc.Execute(context.Background(), affiliate.UpdateAffiliateProfileInput{
		UserID:        "user-1",
		AsaasWalletID: &newWallet,
	}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(wv.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(wv.calls))
	}
	if wv.calls[0].CustomerName != "" || wv.calls[0].CustomerDoc != "" {
		t.Fatalf("expected empty identity when user is missing, got %+v", wv.calls[0])
	}
}
