package businessphone_usecase

import (
	"fmt"
	"log"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/waba"
)

type reconcileChannelStatusUseCase struct {
	partner  businessphone.Dialog360PartnerService
	refs     businessphone.OwnerPhoneReader
	repo     businessphone.Repository
	wabaRepo waba.Repository
}

func NewReconcileChannelStatusUseCase(
	partner businessphone.Dialog360PartnerService,
	refs businessphone.OwnerPhoneReader,
	repo businessphone.Repository,
	wabaRepo waba.Repository,
) businessphone.ChannelStatusReconciler {
	return &reconcileChannelStatusUseCase{partner: partner, refs: refs, repo: repo, wabaRepo: wabaRepo}
}

func (uc *reconcileChannelStatusUseCase) Execute() (businessphone.ChannelStatusReport, error) {
	var report businessphone.ChannelStatusReport

	channels, err := uc.partner.ListChannels()
	if err != nil {
		return report, fmt.Errorf("list partner channels: %w", err)
	}
	byID := make(map[string]businessphone.Dialog360Channel, len(channels))
	for _, ch := range channels {
		byID[ch.ID] = ch
	}

	refs, err := uc.refs.ListDialog360ChannelRefs()
	if err != nil {
		return report, fmt.Errorf("list local channels: %w", err)
	}

	for _, ref := range refs {
		if ref.Dialog360ChannelID == "" {
			continue
		}
		ch, ok := byID[ref.Dialog360ChannelID]

		if !ok || ch.IsDeactivated() {
			if !ref.Active {
				continue
			}
			phone, err := uc.repo.FindByID(ref.PhoneID)
			if err != nil {
				log.Printf("[channel-reconcile] find phone %s: %v", ref.PhoneID, err)
				continue
			}
			if phone.Status == businessphone.StatusSuspended {
				continue
			}
			phone.Status = businessphone.StatusSuspended
			if ok {
				phone.OnboardingError = "deactivated on 360dialog (" + firstNonEmptyStr(ch.HubStatus, ch.ReviewStatus, "cancelled") + ")"
			} else {
				phone.OnboardingError = "channel no longer present on 360dialog"
			}
			if err := uc.repo.Update(phone.ID, phone); err != nil {
				log.Printf("[channel-reconcile] suspend phone %s: %v", phone.ID, err)
				continue
			}
			report.Suspended++
			log.Printf("[channel-reconcile] suspended phone %s (channel %s deactivated/gone)", phone.ID, ref.Dialog360ChannelID)
			continue
		}

		phone, err := uc.repo.FindByID(ref.PhoneID)
		if err != nil {
			log.Printf("[channel-reconcile] find phone %s: %v", ref.PhoneID, err)
			continue
		}

		if (phone.DisplayPhoneNumber == "" && ch.PhoneNumber == "") ||
			(ch.WABAName == "" && uc.wabaUnnamed(phone.WABAId)) {
			if fresh, ferr := uc.partner.GetChannel(ref.Dialog360ChannelID); ferr == nil && fresh != nil {
				ch = mergeChannel(ch, *fresh)
			}
		}

		if applyChannelMetadata(phone, ch) {
			if err := uc.repo.Update(phone.ID, phone); err != nil {
				log.Printf("[channel-reconcile] update phone %s: %v", phone.ID, err)
				continue
			}
			report.Updated++
		}
		if ch.WABAName != "" && uc.backfillWABAName(phone.WABAId, ch.WABAName) {
			report.WABAsNamed++
		}
	}
	return report, nil
}

func applyChannelMetadata(phone *businessphone.WhatsAppBusinessPhoneNumber, ch businessphone.Dialog360Channel) bool {
	changed := false
	if phone.DisplayPhoneNumber == "" && ch.PhoneNumber != "" {
		phone.DisplayPhoneNumber = ch.PhoneNumber
		changed = true
	}
	if phone.VerifiedName == "" && ch.PhoneName != "" {
		phone.VerifiedName = ch.PhoneName
		changed = true
	}
	if ch.QualityRating != "" && string(phone.QualityRating) != ch.QualityRating {
		phone.QualityRating = businessphone.QualityRating(ch.QualityRating)
		changed = true
	}
	if ch.MessagingTier != "" && phone.MessagingLimitTier != ch.MessagingTier {
		phone.MessagingLimitTier = ch.MessagingTier
		changed = true
	}
	if ch.ReviewStatus != "" && phone.AccountReviewStatus != ch.ReviewStatus {
		phone.AccountReviewStatus = ch.ReviewStatus
		changed = true
	}
	return changed
}

func mergeChannel(base, fresh businessphone.Dialog360Channel) businessphone.Dialog360Channel {
	if base.PhoneNumber == "" {
		base.PhoneNumber = fresh.PhoneNumber
	}
	if base.PhoneName == "" {
		base.PhoneName = fresh.PhoneName
	}
	if base.WABAName == "" {
		base.WABAName = fresh.WABAName
	}
	if base.QualityRating == "" {
		base.QualityRating = fresh.QualityRating
	}
	if base.MessagingTier == "" {
		base.MessagingTier = fresh.MessagingTier
	}
	if base.ReviewStatus == "" {
		base.ReviewStatus = fresh.ReviewStatus
	}
	return base
}

func (uc *reconcileChannelStatusUseCase) wabaUnnamed(wabaExternalID string) bool {
	if strings.TrimSpace(wabaExternalID) == "" {
		return false
	}
	rec, err := uc.wabaRepo.FindByMetaWABAId(wabaExternalID)
	return err == nil && rec != nil && strings.TrimSpace(rec.Name) == ""
}

func (uc *reconcileChannelStatusUseCase) backfillWABAName(wabaExternalID, name string) bool {
	if strings.TrimSpace(wabaExternalID) == "" || strings.TrimSpace(name) == "" {
		return false
	}
	rec, err := uc.wabaRepo.FindByMetaWABAId(wabaExternalID)
	if err != nil || rec == nil || strings.TrimSpace(rec.Name) != "" {
		return false
	}
	rec.Name = name
	if err := uc.wabaRepo.Update(rec.ID, rec); err != nil {
		log.Printf("[channel-reconcile] name WABA %s: %v", wabaExternalID, err)
		return false
	}
	return true
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
