package businessphone_usecase

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

func seedPhoneForRelease(repo *mockRepository, id, metaID, wabaID string, status businessphone.Status) *businessphone.WhatsAppBusinessPhoneNumber {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 id,
		MetaPhoneNumberID:  metaID,
		WABAId:             wabaID,
		DisplayPhoneNumber: "+15551234567",
		Status:             status,
		AccessToken:        "stored-token",
	}
	repo.phoneNumbers[id] = phone
	return phone
}

func TestRelease_FullFlow_Connected(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)
	seedWABA(wabaRepo, "waba-internal-1", "waba1")

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "test-token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Deregistered {
		t.Error("expected Deregistered=true")
	}
	if !result.WebhooksRemoved {
		t.Error("expected WebhooksRemoved=true")
	}
	if !result.TokenCleared {
		t.Error("expected TokenCleared=true")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}
	if !result.WABACleanedUp {
		t.Error("expected WABACleanedUp=true")
	}
	if result.DeregisterError != "" {
		t.Errorf("unexpected DeregisterError: %s", result.DeregisterError)
	}
	if result.WebhooksError != "" {
		t.Errorf("unexpected WebhooksError: %s", result.WebhooksError)
	}

	if _, ok := repo.phoneNumbers["p1"]; ok {
		t.Error("phone should have been deleted from repo")
	}

	if _, ok := wabaRepo.accounts["waba-internal-1"]; ok {
		t.Error("orphaned WABA should have been deleted")
	}
}

func TestRelease_NotConnected_SkipsDeregister(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusPending)
	seedWABA(wabaRepo, "waba-internal-1", "waba1")

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "test-token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Deregistered {
		t.Error("expected Deregistered=true (skipped, not an error)")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}
}

func TestRelease_DeregisterFails_ContinuesFlow(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()
	metaAPI.deregisterErr = errors.New("Meta API: 400 bad request")

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)
	seedWABA(wabaRepo, "waba-internal-1", "waba1")

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "test-token",
	})
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Deregistered {
		t.Error("expected Deregistered=false")
	}
	if result.DeregisterError == "" {
		t.Error("expected DeregisterError to be set")
	}

	if !result.WebhooksRemoved {
		t.Error("expected WebhooksRemoved=true despite deregister failure")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true despite deregister failure")
	}
}

func TestRelease_UnsubscribeFails_ContinuesFlow(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()
	metaAPI.unsubscribeErr = errors.New("permission denied")

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "test-token",
	})
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.WebhooksRemoved {
		t.Error("expected WebhooksRemoved=false")
	}
	if result.WebhooksError == "" {
		t.Error("expected WebhooksError to be set")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true despite unsubscribe failure")
	}
}

func TestRelease_EmptyPhoneID(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	_, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "",
		AccessToken: "token",
	})
	if !errors.Is(err, businessphone.ErrPhoneNumberNotFound) {
		t.Errorf("expected ErrPhoneNumberNotFound, got %v", err)
	}
}

// A failed/unsynced number has no Meta access token and no display number. It MUST
// still be removable: the Meta Cloud API steps are skipped, and confirmation falls
// back to the WABA id. (Regression: previously this returned ErrInvalidAccessToken
// and the row was undeletable.)
func TestRelease_NoAccessToken_StillRemovesLocally(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:     "p1",
		WABAId: "1333315365537148",
		Status: businessphone.StatusOnboardingFailed, // never connected, no token, no number
	}

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:            "p1",
		AccessToken:        "",
		ConfirmPhoneNumber: "1333315365537148", // confirm by WABA id (only identifier shown)
	})
	if err != nil {
		t.Fatalf("release without a Meta token must still remove the local record, got: %v", err)
	}
	if !result.PhoneDeleted {
		t.Fatal("expected PhoneDeleted=true")
	}
	if _, ok := repo.phoneNumbers["p1"]; ok {
		t.Fatal("phone row must be gone after release")
	}
}

// The WABA-id confirmation must still reject a wrong value, so the fallback doesn't
// weaken the guard on this irreversible action.
func TestRelease_NumberlessRow_RejectsWrongConfirmation(t *testing.T) {
	repo := newMockRepo()
	uc := NewReleasePhoneUseCase(repo, newMockWABARepo(), newMockMetaAPI(), nil)
	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", WABAId: "1333315365537148", Status: businessphone.StatusOnboardingFailed,
	}
	_, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID: "p1", AccessToken: "", ConfirmPhoneNumber: "999999",
	})
	if !errors.Is(err, businessphone.ErrPhoneConfirmationMismatch) {
		t.Fatalf("wrong confirmation must be rejected, got %v", err)
	}
}

// A dialog360 number has no Meta token, so its billing lives at the 360dialog partner.
// Releasing it MUST cancel the partner channel (client-scoped) or 360dialog bills it
// forever. Regression for the real leak: LePrDkCH stayed "live/Ready" after a delete.
func TestRelease_Dialog360_CancelsPartnerChannel(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	partner := &fakePartnerSvc{}

	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", Provider: businessphone.ProviderDialog360, Status: businessphone.StatusConnected,
		WABAId: "wabaLive", Dialog360ChannelID: "chLive", DisplayPhoneNumber: "+15553395514",
	}
	w := seedWABA(wabaRepo, "waba-int", "wabaLive")
	w.Dialog360ClientID = "clLive"

	uc := NewReleasePhoneUseCase(repo, wabaRepo, newMockMetaAPI(), partner)
	result, err := uc.Execute(businessphone.ReleasePhoneInput{PhoneID: "p1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(partner.cancelled) != 1 || partner.cancelled[0] != "clLive/chLive" {
		t.Fatalf("must cancel the client-scoped channel clLive/chLive, got %v", partner.cancelled)
	}
	if !result.Dialog360Canceled {
		t.Fatal("expected Dialog360Canceled=true")
	}
	if !result.PhoneDeleted {
		t.Fatal("expected PhoneDeleted=true after a successful cancel")
	}
	if _, ok := repo.phoneNumbers["p1"]; ok {
		t.Fatal("phone row must be gone after release")
	}
}

// If the partner cancel fails AND the channel is still live, the release MUST abort and
// keep the local row — deleting it would orphan a paying channel with nothing to retry.
func TestRelease_Dialog360_CancelFails_AbortsAndKeepsRow(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	partner := &fakePartnerSvc{
		cancelErr:     errors.New("360dialog 503"),
		getChannelRes: &businessphone.Dialog360Channel{ID: "chLive", HubStatus: "live"}, // still live
	}

	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", Provider: businessphone.ProviderDialog360, Status: businessphone.StatusConnected,
		WABAId: "wabaLive", Dialog360ChannelID: "chLive", DisplayPhoneNumber: "+15553395514",
	}
	w := seedWABA(wabaRepo, "waba-int", "wabaLive")
	w.Dialog360ClientID = "clLive"

	uc := NewReleasePhoneUseCase(repo, wabaRepo, newMockMetaAPI(), partner)
	result, err := uc.Execute(businessphone.ReleasePhoneInput{PhoneID: "p1"})
	if err == nil {
		t.Fatal("expected an error when the still-live channel could not be cancelled")
	}
	if result.PhoneDeleted {
		t.Fatal("phone must NOT be deleted while its channel is still billing")
	}
	if _, ok := repo.phoneNumbers["p1"]; !ok {
		t.Fatal("phone row must remain so the cancel can be retried")
	}
}

// Idempotency: if the cancel errors but the channel is already gone/cancelled at
// 360dialog, the release proceeds (e.g. cancelled by hand, or a retried release).
func TestRelease_Dialog360_AlreadyGone_ProceedsToDelete(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	partner := &fakePartnerSvc{
		cancelErr:     errors.New("already cancelled"),
		getChannelRes: nil, // GetChannel -> not found -> treat as gone
	}

	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", Provider: businessphone.ProviderDialog360, Status: businessphone.StatusConnected,
		WABAId: "wabaLive", Dialog360ChannelID: "chGone", DisplayPhoneNumber: "+15553395514",
	}
	w := seedWABA(wabaRepo, "waba-int", "wabaLive")
	w.Dialog360ClientID = "clLive"

	uc := NewReleasePhoneUseCase(repo, wabaRepo, newMockMetaAPI(), partner)
	result, err := uc.Execute(businessphone.ReleasePhoneInput{PhoneID: "p1"})
	if err != nil {
		t.Fatalf("an already-gone channel must not block release: %v", err)
	}
	if !result.PhoneDeleted {
		t.Fatal("expected PhoneDeleted=true when the channel is already gone")
	}
}

func TestRelease_PhoneNotFound(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	_, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "nonexistent",
		AccessToken: "token",
	})
	if !errors.Is(err, businessphone.ErrPhoneNumberNotFound) {
		t.Errorf("expected ErrPhoneNumberNotFound, got %v", err)
	}
}

func TestRelease_DeleteFails_ReturnsError(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)
	repo.deleteErr = errors.New("database constraint violation")

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err == nil {
		t.Fatal("expected error from delete failure")
	}

	if !result.Deregistered {
		t.Error("expected Deregistered=true")
	}
	if !result.TokenCleared {
		t.Error("expected TokenCleared=true before delete was attempted")
	}
	if result.PhoneDeleted {
		t.Error("expected PhoneDeleted=false since delete failed")
	}
}

func TestRelease_WABANotOrphaned_KeepsIt(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)
	seedPhoneForRelease(repo, "p2", "meta_p2", "waba1", businessphone.StatusConnected)
	seedWABA(wabaRepo, "waba-internal-1", "waba1")

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}

	if _, ok := wabaRepo.accounts["waba-internal-1"]; !ok {
		t.Error("WABA should NOT have been deleted (still has phone p2)")
	}

	if !result.WABACleanedUp {
		t.Error("expected WABACleanedUp=true (cleanup was attempted)")
	}
}

func TestRelease_NoWABAId_SkipsWebhooksAndCleanup(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	phone := seedPhoneForRelease(repo, "p1", "meta_p1", "", businessphone.StatusConnected)
	phone.WABAId = ""

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.WebhooksRemoved {
		t.Error("expected WebhooksRemoved=false (no WABA to unsubscribe)")
	}
	if result.WABACleanedUp {
		t.Error("expected WABACleanedUp=false (no WABA to cleanup)")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}
}

func TestRelease_NoMetaPhoneNumberID_SkipsDeregister(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	phone := seedPhoneForRelease(repo, "p1", "", "waba1", businessphone.StatusConnected)
	phone.MetaPhoneNumberID = ""

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.DeregisterError != "" {
		t.Errorf("unexpected DeregisterError: %s", result.DeregisterError)
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}
}

func TestRelease_AllMetaCallsFail_StillDeletes(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()
	metaAPI.deregisterErr = errors.New("deregister failed")
	metaAPI.unsubscribeErr = errors.New("unsubscribe failed")

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Deregistered {
		t.Error("expected Deregistered=false")
	}
	if result.WebhooksRemoved {
		t.Error("expected WebhooksRemoved=false")
	}
	if result.DeregisterError == "" {
		t.Error("expected DeregisterError to be set")
	}
	if result.WebhooksError == "" {
		t.Error("expected WebhooksError to be set")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true despite all Meta failures")
	}
	if !result.TokenCleared {
		t.Error("expected TokenCleared=true despite all Meta failures")
	}
}

func TestRelease_ClearTokenFails_ContinuesFlow(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)

	repo.updateErr = errors.New("db update error")

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.TokenCleared {
		t.Error("expected TokenCleared=false since it failed")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true despite clear token failure")
	}
}

func TestRelease_DisconnectedStatus(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusDisconnected)

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Deregistered {
		t.Error("expected Deregistered=true (skipped for non-connected status)")
	}
}

func TestRelease_BannedStatus(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusBanned)

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Deregistered {
		t.Error("expected Deregistered=true (skipped for banned status)")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}
}

func TestRelease_ClearsWABATokenBeforeDelete(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusConnected)
	w := seedWABA(wabaRepo, "waba-int-1", "waba1")
	w.AccessToken = "secret-waba-token"

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.WABACleanedUp {
		t.Error("expected WABACleanedUp=true")
	}

	if _, ok := wabaRepo.accounts["waba-int-1"]; ok {
		t.Error("orphaned WABA should have been deleted")
	}
}

func TestRelease_FlaggedStatus(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	metaAPI := newMockMetaAPI()

	seedPhoneForRelease(repo, "p1", "meta_p1", "waba1", businessphone.StatusFlagged)

	uc := NewReleasePhoneUseCase(repo, wabaRepo, metaAPI, nil)

	result, err := uc.Execute(businessphone.ReleasePhoneInput{
		PhoneID:     "p1",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Deregistered {
		t.Error("expected Deregistered=true (non-connected status is skipped)")
	}
	if !result.PhoneDeleted {
		t.Error("expected PhoneDeleted=true")
	}
}
