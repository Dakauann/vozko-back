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

func TestOnboardEmbeddedSignup_NewPhoneNeedsNoSlot(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	gate := &stubProvisioningGate{ok: false}
	uc := NewOnboardEmbeddedSignupUseCase(repo, wabaRepo, newMockMetaAPI(), gate)

	res, err := uc.Execute(quotaInput("ws-1", "meta-new"))
	if err != nil {
		t.Fatalf("a client-direct number consumes no provisioning slot, got %v", err)
	}
	if !res.IsNew || len(repo.phoneNumbers) != 1 {
		t.Fatalf("expected one new phone, got isNew=%v count=%d", res.IsNew, len(repo.phoneNumbers))
	}
	if gate.calls != 0 {
		t.Fatalf("the 360dialog entitlement must not be consulted here, got %d calls", gate.calls)
	}
}

func TestOnboardEmbeddedSignup_ReconnectOfOwnedActivePhoneIsAllowed(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusConnected)
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), &stubProvisioningGate{ok: false})

	res, err := uc.Execute(quotaInput("ws-1", "meta-1"))
	if err != nil {
		t.Fatalf("reconnecting an owned phone must not be blocked, got %v", err)
	}
	if res.IsNew || res.Phone.ID != "p1" {
		t.Fatalf("expected the existing phone to be updated, got %+v", res)
	}
}

func TestOnboardEmbeddedSignup_OwnedSuspendedPhoneMayBeRevived(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusSuspended)
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), &stubProvisioningGate{ok: false})

	if _, err := uc.Execute(quotaInput("ws-1", "meta-1")); err != nil {
		t.Fatalf("a workspace may reconnect its own suspended number, got %v", err)
	}
}

func TestOnboardEmbeddedSignup_PhoneOwnedByAnotherWorkspaceIsRefused(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-other", businessphone.StatusConnected)
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), &stubProvisioningGate{ok: true})

	_, err := uc.Execute(quotaInput("ws-1", "meta-1"))
	if !errors.Is(err, businessphone.ErrPhoneHeldByAnotherWorkspace) {
		t.Fatalf("expected ErrPhoneHeldByAnotherWorkspace, got %v", err)
	}
	if repo.phoneNumbers["p1"].OwnerWorkspaceID != "ws-other" {
		t.Fatalf("a refused onboarding must not move ownership")
	}
}

func TestOnboardEmbeddedSignup_AuthorizeMatchesExecuteRule(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = ownedPhone("p1", "meta-1", "ws-1", businessphone.StatusConnected)
	repo.phoneNumbers["p2"] = ownedPhone("p2", "meta-2", "ws-other", businessphone.StatusConnected)
	uc := NewOnboardEmbeddedSignupUseCase(repo, newMockWABARepo(), newMockMetaAPI(), &stubProvisioningGate{ok: false})

	if err := uc.Authorize("ws-1", "meta-1"); err != nil {
		t.Fatalf("owned phone must be authorized, got %v", err)
	}
	if err := uc.Authorize("ws-1", "meta-new"); err != nil {
		t.Fatalf("an unknown number must be authorized without a slot, got %v", err)
	}
	if err := uc.Authorize("ws-1", "meta-2"); !errors.Is(err, businessphone.ErrPhoneHeldByAnotherWorkspace) {
		t.Fatalf("another workspace's number must be refused, got %v", err)
	}
}
