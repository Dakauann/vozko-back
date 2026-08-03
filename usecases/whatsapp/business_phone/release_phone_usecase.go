package businessphone_usecase

import (
	"fmt"
	"log"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/waba"
)

type releasePhoneUseCase struct {
	repo       businessphone.Repository
	wabaRepo   waba.Repository
	metaClient businessphone.MetaAPIService
	partner    businessphone.Dialog360PartnerService // dialog360 channel cancellation; may be nil
}

func NewReleasePhoneUseCase(
	repo businessphone.Repository,
	wabaRepo waba.Repository,
	metaClient businessphone.MetaAPIService,
	partner businessphone.Dialog360PartnerService,
) businessphone.ReleasePhoneUseCase {
	return &releasePhoneUseCase{
		repo:       repo,
		wabaRepo:   wabaRepo,
		metaClient: metaClient,
		partner:    partner,
	}
}

func (uc *releasePhoneUseCase) Execute(input businessphone.ReleasePhoneInput) (*businessphone.ReleasePhoneResult, error) {
	if input.PhoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}

	phone, err := uc.repo.FindByID(input.PhoneID)
	if err != nil {
		return nil, err
	}

	// A Meta access token is NOT required to remove the local record. Failed
	// onboardings and dialog360-hosted numbers never have one, and they must still
	// be removable. When a token is present the Meta Cloud API teardown runs below;
	// when absent, those steps are skipped and only the local teardown happens.
	accessToken := input.AccessToken
	if accessToken == "" {
		accessToken = phone.AccessToken
	}

	// Typed-confirmation guard. Match the display number, or the WABA id when the
	// number was never populated (a failed/unsynced row shows no number), otherwise
	// such a phone could never be confirmed and thus never removed. The backend owns
	// this check so the irreversible teardown can't be triggered against the wrong
	// record even if a client skips its own confirmation UI.
	if input.ConfirmPhoneNumber != "" {
		target := phone.DisplayPhoneNumber
		if strings.TrimSpace(target) == "" {
			target = phone.WABAId
		}
		want := normalizePhoneDigits(target)
		got := normalizePhoneDigits(input.ConfirmPhoneNumber)
		if want == "" || want != got {
			return nil, businessphone.ErrPhoneConfirmationMismatch
		}
	}

	result := &businessphone.ReleasePhoneResult{}

	log.Printf("[release-phone] Starting release for phone %s (meta_id=%s, waba=%s)", phone.ID, phone.MetaPhoneNumberID, phone.WABAId)

	if accessToken != "" && phone.MetaPhoneNumberID != "" && phone.Status == businessphone.StatusConnected {
		if err := uc.metaClient.DeregisterPhone(phone.MetaPhoneNumberID, accessToken); err != nil {
			log.Printf("[release-phone] Failed to deregister phone %s from Cloud API: %v (continuing)", phone.ID, err)
			result.DeregisterError = err.Error()
		} else {
			result.Deregistered = true
			log.Printf("[release-phone] Deregistered phone %s from Cloud API", phone.ID)
		}
	} else if phone.Status != businessphone.StatusConnected {
		log.Printf("[release-phone] Phone %s not connected (status=%s), skipping deregister", phone.ID, phone.Status)
		result.Deregistered = true
	}

	if accessToken != "" && phone.WABAId != "" {
		if err := uc.metaClient.UnsubscribeApp(phone.WABAId, accessToken); err != nil {
			log.Printf("[release-phone] Failed to unsubscribe app from WABA %s: %v (continuing)", phone.WABAId, err)
			result.WebhooksError = err.Error()
		} else {
			result.WebhooksRemoved = true
			log.Printf("[release-phone] Unsubscribed app from WABA %s webhooks", phone.WABAId)
		}
	}

	// dialog360-hosted numbers have no Meta token, so the Cloud API teardown above is a
	// no-op for them, their billing lives at the 360dialog PARTNER, not Meta. We MUST
	// cancel the partner channel here, or the number is billed forever after the local
	// row is gone. Cancel FIRST and abort the local delete if it fails, so a transient
	// 360dialog error can never orphan a paying channel with no local record to retry.
	if phone.Provider.IsDialog360() && strings.TrimSpace(phone.Dialog360ChannelID) != "" {
		if err := uc.cancelDialog360Channel(phone); err != nil {
			log.Printf("[release-phone] ABORTING release of phone %s: could not cancel 360dialog channel %s: %v", phone.ID, phone.Dialog360ChannelID, err)
			return result, err
		}
		result.Dialog360Canceled = true
		log.Printf("[release-phone] Cancelled 360dialog channel %s for phone %s", phone.Dialog360ChannelID, phone.ID)
	}

	if err := uc.repo.ClearAccessToken(phone.ID); err != nil {
		log.Printf("[release-phone] Failed to clear phone access token: %v (continuing)", err)
	} else {
		result.TokenCleared = true
		log.Printf("[release-phone] Cleared access token for phone %s", phone.ID)
	}

	if err := uc.repo.Delete(phone.ID); err != nil {
		log.Printf("[release-phone] Failed to delete phone %s from DB: %v", phone.ID, err)
		return result, err
	}
	result.PhoneDeleted = true
	log.Printf("[release-phone] Deleted phone %s from local DB", phone.ID)

	if phone.WABAId != "" {
		uc.cleanupOrphanedWABA(phone.WABAId)
		result.WABACleanedUp = true
	}

	log.Printf("[release-phone] Phone %s released successfully", phone.ID)
	return result, nil
}

// cancelDialog360Channel cancels the phone's channel at the 360dialog partner so it
// stops billing. It is idempotent: if the cancel call fails but a fresh read shows the
// channel is already cancelled/gone, that counts as success. Any other failure is
// returned so the caller aborts the local delete (never orphan a billing channel).
func (uc *releasePhoneUseCase) cancelDialog360Channel(phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	if uc.partner == nil {
		return fmt.Errorf("cannot cancel 360dialog channel %s: partner service unavailable", phone.Dialog360ChannelID)
	}

	clientID := uc.resolveDialog360ClientID(phone)
	if clientID == "" {
		return fmt.Errorf("cannot cancel 360dialog channel %s: no 360dialog client id for WABA %s", phone.Dialog360ChannelID, phone.WABAId)
	}

	err := uc.partner.CancelChannel(clientID, phone.Dialog360ChannelID)
	if err == nil {
		return nil
	}

	// The cancel call errored, confirm the true state before deciding it's fatal. If
	// the channel is already cancelled/deactivated or no longer present, the goal is
	// met and the local delete may proceed (idempotent re-release, or cancelled by hand).
	if ch, gerr := uc.partner.GetChannel(phone.Dialog360ChannelID); gerr == nil && (ch == nil || ch.IsDeactivated()) {
		log.Printf("[release-phone] 360dialog channel %s already cancelled/gone; proceeding", phone.Dialog360ChannelID)
		return nil
	}
	return fmt.Errorf("cancel 360dialog channel %s: %w", phone.Dialog360ChannelID, err)
}

// resolveDialog360ClientID finds the partner client id needed for a client-scoped
// cancel. The id lives on the WABA record (joined by meta_waba_id), mirroring how the
// reconcilers resolve it.
func (uc *releasePhoneUseCase) resolveDialog360ClientID(phone *businessphone.WhatsAppBusinessPhoneNumber) string {
	if strings.TrimSpace(phone.WABAId) == "" {
		return ""
	}
	rec, err := uc.wabaRepo.FindByMetaWABAId(phone.WABAId)
	if err != nil || rec == nil {
		return ""
	}
	return strings.TrimSpace(rec.Dialog360ClientID)
}

// normalizePhoneDigits reduces a phone number to its digits so the typed
// confirmation tolerates formatting differences (spaces, "+", dashes, parens).
func normalizePhoneDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (uc *releasePhoneUseCase) cleanupOrphanedWABA(wabaID string) {
	remaining, err := uc.repo.FindByWABAId(wabaID)
	if err != nil {
		log.Printf("[release-phone] Failed to check remaining phones for WABA %s: %v", wabaID, err)
		return
	}

	if len(remaining) > 0 {
		log.Printf("[release-phone] WABA %s still has %d phone(s), keeping it", wabaID, len(remaining))
		return
	}

	wabaRecord, err := uc.wabaRepo.FindByMetaWABAId(wabaID)
	if err != nil {
		log.Printf("[release-phone] WABA record %s not found locally, nothing to clean up", wabaID)
		return
	}

	if err := uc.wabaRepo.ClearAccessToken(wabaRecord.ID); err != nil {
		log.Printf("[release-phone] Failed to clear WABA %s access token: %v", wabaRecord.ID, err)
	}

	if err := uc.wabaRepo.Delete(wabaRecord.ID); err != nil {
		log.Printf("[release-phone] Failed to delete orphaned WABA %s: %v", wabaRecord.ID, err)
		return
	}

	log.Printf("[release-phone] Deleted orphaned WABA %s (meta_id=%s)", wabaRecord.ID, wabaID)
}
