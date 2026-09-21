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
	partner    businessphone.Dialog360PartnerService
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

	accessToken := input.AccessToken
	if accessToken == "" {
		accessToken = phone.AccessToken
	}

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
		uc.cleanupOrphanedWABA(phone.WABAId, accessToken, result)
		result.WABACleanedUp = true
	}

	log.Printf("[release-phone] Phone %s released successfully", phone.ID)
	return result, nil
}

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

	if ch, gerr := uc.partner.GetChannel(phone.Dialog360ChannelID); gerr == nil && (ch == nil || ch.IsDeactivated()) {
		log.Printf("[release-phone] 360dialog channel %s already cancelled/gone; proceeding", phone.Dialog360ChannelID)
		return nil
	}
	return fmt.Errorf("cancel 360dialog channel %s: %w", phone.Dialog360ChannelID, err)
}

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

func normalizePhoneDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (uc *releasePhoneUseCase) cleanupOrphanedWABA(wabaID, accessToken string, result *businessphone.ReleasePhoneResult) {
	remaining, err := uc.repo.FindByWABAId(wabaID)
	if err != nil {
		log.Printf("[release-phone] Failed to check remaining phones for WABA %s: %v", wabaID, err)
		return
	}

	if len(remaining) > 0 {
		log.Printf("[release-phone] WABA %s still has %d phone(s), keeping it (webhooks stay subscribed)", wabaID, len(remaining))
		return
	}

	if accessToken != "" {
		if err := uc.metaClient.UnsubscribeApp(wabaID, accessToken); err != nil {
			log.Printf("[release-phone] Failed to unsubscribe app from WABA %s: %v (continuing)", wabaID, err)
			result.WebhooksError = err.Error()
		} else {
			result.WebhooksRemoved = true
			log.Printf("[release-phone] Unsubscribed app from WABA %s webhooks (no phones left)", wabaID)
		}
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
