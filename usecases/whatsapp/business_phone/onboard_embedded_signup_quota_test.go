package businessphone_usecase

import (
	"errors"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type stubProvisioningGate struct {
	ok    bool
	err   error
	calls int
}

func (g *stubProvisioningGate) CanProvisionPhone(string) (bool, error) {
	g.calls++
	return g.ok, g.err
}

func quotaInput(workspaceID, metaPhoneID string) businessphone.OnboardEmbeddedSignupInput {
	return businessphone.OnboardEmbeddedSignupInput{
		Provider:         businessphone.ProviderMeta,
		PhoneNumberID:    metaPhoneID,
		WABAId:           "waba-1",
		OwnerWorkspaceID: workspaceID,
		OwnerAssignedBy:  "user-1",
		AccessToken:      "token",
	}
}

func ownedPhone(id, metaPhoneID, workspaceID string, status businessphone.Status) *businessphone.WhatsAppBusinessPhoneNumber {
	at := time.Now().UTC()
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                id,
		Provider:          businessphone.ProviderMeta,
		MetaPhoneNumberID: metaPhoneID,
		WABAId:            "waba-1",
		Status:            status,
		OwnerWorkspaceID:  workspaceID,
		OwnerAssignedBy:   "user-1",
		OwnerAssignedAt:   &at,
	}
}

func TestOnboardEmbeddedSignup_RefusesNewPhoneOverQuota(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, wabaRepo, newMockMetaAPI(), gate)

	_, err := uc.Execute(quotaInput("ws-1", "meta-new"))
	if !errors.Is(err, businessphone.ErrPhoneLimitReached) {
		t.Fatalf("expected ErrPhoneLimitReached, got %v", err)
	}
	if len(repo.phoneNumbers) != 0 {
		t.Fatalf("no phone record may be created over quota, got %d", len(repo.phoneNumbers))
	}
	if len(wabaRepo.accounts) != 0 {
		t.Fatalf("no WABA record may be created over quota, got %d", len(wabaRepo.accounts))
	}
}

func TestOnboardEmbeddedSignup_AllowsNewPhoneUnderQuota(t *testing.T) {
	repo := newMockRepo()
	gate := &stubProvisioningGate{ok: true}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	res, err := uc.Execute(quotaInput("ws-1", "meta-new"))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !res.IsNew || len(repo.phoneNumbers) != 1 {
		t.Fatalf("expected one new phone, got isNew=%v count=%d", res.IsNew, len(repo.phoneNumbers))
	}
	if gate.calls != 1 {
		t.Fatalf("expected the gate to be consulted once, got %d", gate.calls)
	}
}

func TestOnboardEmbeddedSignup_ReconnectOfOwnedActivePhoneSkipsQuota(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusConnected)
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	res, err := uc.Execute(quotaInput("ws-1", "meta-1"))
	if err != nil {
		t.Fatalf("reconnecting an owned phone must not be blocked, got %v", err)
	}
	if res.IsNew || res.Phone.ID != "p1" {
		t.Fatalf("expected the existing phone to be updated, got %+v", res)
	}
	if gate.calls != 0 {
		t.Fatalf("an owned active phone must not consult the gate, got %d calls", gate.calls)
	}
}

func TestOnboardEmbeddedSignup_ReconnectOfOwnedDisconnectedPhoneSkipsQuota(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusDisconnected)
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	if _, err := uc.Execute(quotaInput("ws-1", "meta-1")); err != nil {
		t.Fatalf("a phone that already counts toward the quota must reconnect, got %v", err)
	}
}

func TestOnboardEmbeddedSignup_PhoneOwnedByAnotherWorkspaceIsGated(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-other", businessphone.StatusConnected)
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	_, err := uc.Execute(quotaInput("ws-1", "meta-1"))
	if !errors.Is(err, businessphone.ErrPhoneLimitReached) {
		t.Fatalf("expected ErrPhoneLimitReached, got %v", err)
	}
	if repo.phoneNumbers["p1"].OwnerWorkspaceID != "ws-other" {
		t.Fatalf("a refused onboarding must not move ownership")
	}
}

func TestOnboardEmbeddedSignup_OwnedSuspendedPhoneIsGated(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusSuspended)
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	_, err := uc.Execute(quotaInput("ws-1", "meta-1"))
	if !errors.Is(err, businessphone.ErrPhoneLimitReached) {
		t.Fatalf("a suspended phone does not count toward the quota, so reviving it must be gated; got %v", err)
	}
	if repo.phoneNumbers["p1"].Status != businessphone.StatusSuspended {
		t.Fatalf("a refused onboarding must not change the phone status")
	}
}

func TestOnboardEmbeddedSignup_SoftDeletedPhoneIsGated(t *testing.T) {
	repo := newMockRepo()
	repo.deletedPhoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusConnected)
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	_, err := uc.Execute(quotaInput("ws-1", "meta-1"))
	if !errors.Is(err, businessphone.ErrPhoneLimitReached) {
		t.Fatalf("expected ErrPhoneLimitReached, got %v", err)
	}
	if _, restored := repo.phoneNumbers["p1"]; restored {
		t.Fatalf("a refused onboarding must not restore a deleted phone")
	}
}

func TestOnboardEmbeddedSignup_GateErrorFailsClosed(t *testing.T) {
	repo := newMockRepo()
	boom := errors.New("no subscription")
	gate := &stubProvisioningGate{ok: true, err: boom}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	_, err := uc.Execute(quotaInput("ws-1", "meta-new"))
	if !errors.Is(err, boom) {
		t.Fatalf("expected the gate error to propagate, got %v", err)
	}
	if len(repo.phoneNumbers) != 0 {
		t.Fatalf("no phone may be created when the gate cannot be evaluated")
	}
}

func TestOnboardEmbeddedSignup_AuthorizeMatchesExecuteRule(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusConnected)
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), gate)

	if err := uc.Authorize("ws-1", "meta-1"); err != nil {
		t.Fatalf("owned active phone must be authorized, got %v", err)
	}
	if err := uc.Authorize("ws-1", "meta-new"); !errors.Is(err, businessphone.ErrPhoneLimitReached) {
		t.Fatalf("new phone over quota must be refused, got %v", err)
	}
}
