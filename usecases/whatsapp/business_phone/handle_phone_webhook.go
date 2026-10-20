package businessphone_usecase

import (
	"errors"
	"log"
	"strings"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/optout"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/waba"
)

type handlePhoneWebhook struct {
	repo     businessphone.Repository
	wabaRepo waba.Repository
	recorder quality.Recorder
	optOuts  optout.Recorder
	notices  businessphone.PhoneNotifier
	now      func() time.Time
}

var _ businessphone.HandlePhoneWebhookUseCase = (*handlePhoneWebhook)(nil)

func NewHandlePhoneWebhook(repo businessphone.Repository, wabaRepo waba.Repository, recorder quality.Recorder, optOuts optout.Recorder) *handlePhoneWebhook {
	return &handlePhoneWebhook{repo: repo, wabaRepo: wabaRepo, recorder: recorder, optOuts: optOuts, now: time.Now}
}

func (h *handlePhoneWebhook) WithNotices(notices businessphone.PhoneNotifier) *handlePhoneWebhook {
	h.notices = notices
	return h
}

func (h *handlePhoneWebhook) notifyPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, notice businessphone.PhoneNotice) {
	if h.notices == nil {
		return
	}
	if err := h.notices.NotifyPhone(phone, notice); err != nil {
		log.Printf("[phone-webhook] %s notification for phone %s failed: %v", notice.State, phone.ID, err)
	}
}

func (h *handlePhoneWebhook) clearPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, states ...string) {
	if h.notices == nil {
		return
	}
	if err := h.notices.ClearPhone(phone, states...); err != nil {
		log.Printf("[phone-webhook] alerts %v of phone %s could not be closed: %v", states, phone.ID, err)
	}
}

func (h *handlePhoneWebhook) Execute(payload *businessphone.PhoneWebhookPayload) error {
	if payload == nil {
		return nil
	}

	var recordErrs []error
	for _, entry := range payload.Entry {
		occurredAt := quality.OccurredAt(entry.Time, h.now())
		for _, change := range entry.Changes {
			switch change.Field {
			case businessphone.FieldPhoneNumberQualityUpdate:
				recordErrs = append(recordErrs, h.handleQualityUpdate(change.Value, occurredAt))
			case optout.WebhookField:
				recordErrs = append(recordErrs, h.recordUserPreferences(change.Value, occurredAt))
			case businessphone.FieldPhoneNumberNameUpdate:
				h.handleNameUpdate(change.Value)
			case businessphone.FieldAccountAlerts:
				h.handleAccountAlerts(change.Value)
			case businessphone.FieldBusinessCapabilityUpdate:
				h.handleCapabilityUpdate(entry.ID, change.Value)
			case businessphone.FieldAccountUpdate:
				h.handleAccountUpdate(entry.ID, change.Value)
			case businessphone.FieldAccountReviewUpdate:
				h.handleAccountReviewUpdate(entry.ID, change.Value)
			case businessphone.FieldBusinessStatusUpdate:
				h.handleBusinessStatusUpdate(entry.ID, change.Value)
			default:
				log.Printf("[phone-webhook] unhandled field: %s", change.Field)
			}
		}
	}
	return errors.Join(recordErrs...)
}

func (h *handlePhoneWebhook) handleQualityUpdate(v businessphone.PhoneWebhookValue, occurredAt time.Time) error {
	phoneNumber := normalizePhoneNumber(v.DisplayPhoneNumber)
	if phoneNumber == "" {
		log.Printf("[phone-webhook] quality_update: missing display_phone_number")
		return nil
	}

	phone, err := h.repo.FindByDisplayPhoneNumber(phoneNumber)
	if err != nil {
		log.Printf("[phone-webhook] quality_update: phone %s not found: %v", phoneNumber, err)
		return nil
	}

	recordErr := h.recordQualityEvent(phone, v, occurredAt)
	changed := false

	if v.CurrentLimit != "" && phone.MessagingLimitTier != v.CurrentLimit {
		phone.MessagingLimitTier = v.CurrentLimit
		changed = true
	}

	switch strings.ToUpper(v.Event) {
	case "FLAGGED":
		if phone.Status != businessphone.StatusFlagged {
			phone.Status = businessphone.StatusFlagged
			changed = true
		}
	case "RESTRICTED":
		if phone.Status != businessphone.StatusRestricted {
			phone.Status = businessphone.StatusRestricted
			changed = true
		}
	case "UNFLAGGED":
		if phone.Status == businessphone.StatusFlagged || phone.Status == businessphone.StatusRestricted {
			phone.Status = businessphone.StatusConnected
			changed = true
		}
	}

	if changed {
		if err := h.repo.Update(phone.ID, phone); err != nil {
			log.Printf("[phone-webhook] quality_update: failed to update phone %s: %v", phoneNumber, err)
		} else {
			log.Printf("[phone-webhook] quality_update: phone %s updated (event=%s, limit=%s)", phoneNumber, v.Event, v.CurrentLimit)
			if phone.Status == businessphone.StatusFlagged || phone.Status == businessphone.StatusRestricted {
				h.notifyPhone(phone, businessphone.QualityNotice(phone.Status == businessphone.StatusRestricted))
			} else if phone.Status == businessphone.StatusConnected {
				h.clearPhone(phone, businessphone.QualityNoticeStates...)
			}
		}
	}
	return recordErr
}

func (h *handlePhoneWebhook) recordUserPreferences(v businessphone.PhoneWebhookValue, fallback time.Time) error {
	var events []optout.Event
	for _, p := range v.UserPreferences {
		preference, ok := optout.ParsePreference(p.Value)
		if !ok || strings.TrimSpace(p.WaID) == "" {
			continue
		}
		event := optout.Event{
			PhoneNumberID: v.Metadata.PhoneNumberID,
			RecipientHash: optout.HashRecipient(p.WaID),
			Category:      strings.TrimSpace(p.Category),
			Preference:    preference,
			OccurredAt:    quality.OccurredAt(p.Timestamp, fallback),
		}
		if event.Validate() == nil {
			events = append(events, event)
		}
	}
	return h.optOuts.Record(events...)
}

func (h *handlePhoneWebhook) recordQualityEvent(phone *businessphone.WhatsAppBusinessPhoneNumber, v businessphone.PhoneWebhookValue, occurredAt time.Time) error {
	if strings.TrimSpace(phone.MetaPhoneNumberID) == "" {
		return nil
	}
	base := quality.Change{
		Subject:    quality.SubjectPhone,
		ExternalID: phone.MetaPhoneNumberID,
		WABAID:     phone.WABAId,
		Name:       phone.DisplayPhoneNumber,
		Event:      strings.ToUpper(strings.TrimSpace(v.Event)),
		Source:     quality.SourceWebhook,
		OccurredAt: occurredAt,
	}
	var changes []quality.Change
	if base.Event != "" {
		status := base
		status.Signal, status.Current = quality.SignalStatus, base.Event
		changes = append(changes, status)
	}
	limit := base
	limit.Signal = quality.SignalMessagingLimit
	if moved, ok := limit.Moved(phone.MessagingLimitTier, v.CurrentLimit); ok {
		changes = append(changes, moved)
	}
	return h.recorder.Record(changes...)
}

func (h *handlePhoneWebhook) handleNameUpdate(v businessphone.PhoneWebhookValue) {
	phoneNumber := normalizePhoneNumber(v.DisplayPhoneNumber)
	if phoneNumber == "" {
		log.Printf("[phone-webhook] name_update: missing display_phone_number")
		return
	}

	phone, err := h.repo.FindByDisplayPhoneNumber(phoneNumber)
	if err != nil {
		log.Printf("[phone-webhook] name_update: phone %s not found: %v", phoneNumber, err)
		return
	}

	changed := false

	decision := strings.ToUpper(v.Decision)
	switch decision {
	case "APPROVED":
		if v.RequestedVerifiedName != "" && phone.VerifiedName != v.RequestedVerifiedName {
			phone.VerifiedName = v.RequestedVerifiedName
			changed = true
		}
		if phone.NameStatus != businessphone.NameStatusApproved {
			phone.NameStatus = businessphone.NameStatusApproved
			changed = true
		}
	case "REJECTED", "DECLINED":
		if phone.NameStatus != businessphone.NameStatusDeclined {
			phone.NameStatus = businessphone.NameStatusDeclined
			changed = true
		}
	}

	if changed {
		if err := h.repo.Update(phone.ID, phone); err != nil {
			log.Printf("[phone-webhook] name_update: failed to update phone %s: %v", phoneNumber, err)
		} else {
			log.Printf("[phone-webhook] name_update: phone %s updated (decision=%s, name=%s)", phoneNumber, decision, v.RequestedVerifiedName)
			switch decision {
			case "APPROVED":
				h.notifyPhone(phone, businessphone.NameApprovedNotice(phone.VerifiedName))
			case "REJECTED", "DECLINED":
				h.notifyPhone(phone, businessphone.NameRejectedNotice())
			}
		}
	}
}

func (h *handlePhoneWebhook) handleAccountAlerts(v businessphone.PhoneWebhookValue) {
	phoneNumber := normalizePhoneNumber(v.PhoneNumber)
	if phoneNumber == "" {
		phoneNumber = normalizePhoneNumber(v.DisplayPhoneNumber)
	}
	if phoneNumber == "" {
		log.Printf("[phone-webhook] account_alerts: missing phone_number (event=%s)", v.Event)
		return
	}

	phone, err := h.repo.FindByDisplayPhoneNumber(phoneNumber)
	if err != nil {
		log.Printf("[phone-webhook] account_alerts: phone %s not found: %v", phoneNumber, err)
		return
	}

	changed := false
	bannedNow := false

	if v.CurrentLimit != "" && phone.MessagingLimitTier != v.CurrentLimit {
		phone.MessagingLimitTier = v.CurrentLimit
		changed = true
	}

	event := strings.ToUpper(v.Event)
	switch event {
	case "VERIFIED_ACCOUNT":
		if !phone.IsOfficialBusiness {
			phone.IsOfficialBusiness = true
			changed = true
		}
	case "DISABLED_UPDATE":
		if phone.Status != businessphone.StatusBanned {
			phone.Status = businessphone.StatusBanned
			changed = true
			bannedNow = true
		}
	}

	if changed {
		if err := h.repo.Update(phone.ID, phone); err != nil {
			log.Printf("[phone-webhook] account_alerts: failed to update phone %s: %v", phoneNumber, err)
		} else {
			log.Printf("[phone-webhook] account_alerts: phone %s updated (event=%s)", phoneNumber, event)
			if bannedNow {
				h.notifyPhone(phone, businessphone.BannedNotice())
			}
		}
	}
}

func (h *handlePhoneWebhook) handleCapabilityUpdate(wabaID string, v businessphone.PhoneWebhookValue) {
	if wabaID == "" {
		log.Printf("[phone-webhook] capability_update: missing waba_id")
		return
	}

	phones, err := h.repo.FindByWABAId(wabaID)
	if err != nil {
		log.Printf("[phone-webhook] capability_update: failed to find phones for waba %s: %v", wabaID, err)
		return
	}

	tier := conversationLimitToTier(v.MaxDailyConversationPerPhone)
	if tier == "" {
		return
	}

	for _, phone := range phones {
		if phone.MessagingLimitTier != tier {
			phone.MessagingLimitTier = tier
			if err := h.repo.Update(phone.ID, phone); err != nil {
				log.Printf("[phone-webhook] capability_update: failed to update phone %s: %v", phone.DisplayPhoneNumber, err)
			}
		}
	}

	log.Printf("[phone-webhook] capability_update: waba %s updated %d phones (tier=%s)", wabaID, len(phones), tier)

	if wabaEntity, err := h.wabaRepo.FindByMetaWABAId(wabaID); err == nil && wabaEntity != nil {
		if wabaEntity.MessagingLimitTier != tier {
			wabaEntity.MessagingLimitTier = tier
			if err := h.wabaRepo.Update(wabaEntity.ID, wabaEntity); err != nil {
				log.Printf("[phone-webhook] capability_update: failed to update WABA entity %s: %v", wabaID, err)
			}
		}
	}
}

func (h *handlePhoneWebhook) handleAccountUpdate(wabaID string, v businessphone.PhoneWebhookValue) {
	event := strings.ToUpper(v.Event)

	if event == "PARTNER_ADDED" {
		wabaInfoID := ""
		if v.WABAInfo != nil {
			wabaInfoID = v.WABAInfo.WABAId
		}
		log.Printf("[phone-webhook] account_update: PARTNER_ADDED for waba=%s (handled by embedded signup)", wabaInfoID)
		return
	}

	if event == "VERIFIED_ACCOUNT" {
		phoneNumber := normalizePhoneNumber(v.PhoneNumber)
		if phoneNumber == "" {
			phoneNumber = normalizePhoneNumber(v.DisplayPhoneNumber)
		}
		if phoneNumber == "" {
			log.Printf("[phone-webhook] account_update: VERIFIED_ACCOUNT missing phone_number")
			return
		}
		phone, err := h.repo.FindByDisplayPhoneNumber(phoneNumber)
		if err != nil {
			log.Printf("[phone-webhook] account_update: VERIFIED_ACCOUNT phone %s not found: %v", phoneNumber, err)
			return
		}
		if !phone.IsOfficialBusiness {
			phone.IsOfficialBusiness = true
			if err := h.repo.Update(phone.ID, phone); err != nil {
				log.Printf("[phone-webhook] account_update: failed to update phone %s: %v", phoneNumber, err)
			} else {
				log.Printf("[phone-webhook] account_update: phone %s marked as official business", phoneNumber)
				h.notifyPhone(phone, businessphone.VerifiedNotice())
			}
		}
		return
	}

	if v.BanInfo != nil {
		switch strings.ToUpper(v.BanInfo.WABABanState) {
		case "SCHEDULE_FOR_DISABLE":
			log.Printf("[phone-webhook] account_update: waba %s scheduled for disable (date=%s)", wabaID, v.BanInfo.WABABanDate)

			if wabaID != "" {
				phones, err := h.repo.FindByWABAId(wabaID)
				if err != nil {
					log.Printf("[phone-webhook] account_update: failed to find phones for waba %s: %v", wabaID, err)
					return
				}
				for _, phone := range phones {
					if phone.Status != businessphone.StatusFlagged {
						phone.Status = businessphone.StatusFlagged
						if err := h.repo.Update(phone.ID, phone); err != nil {
							log.Printf("[phone-webhook] account_update: failed to flag phone %s: %v", phone.DisplayPhoneNumber, err)
						} else {
							h.notifyPhone(phone, businessphone.ScheduledDisableNotice(v.BanInfo.WABABanDate))
						}
					}
				}
			}
		case "DISABLE":
			if wabaID != "" {
				phones, err := h.repo.FindByWABAId(wabaID)
				if err != nil {
					log.Printf("[phone-webhook] account_update: failed to find phones for waba %s: %v", wabaID, err)
					return
				}
				for _, phone := range phones {
					if phone.Status != businessphone.StatusBanned {
						phone.Status = businessphone.StatusBanned
						if err := h.repo.Update(phone.ID, phone); err != nil {
							log.Printf("[phone-webhook] account_update: failed to ban phone %s: %v", phone.DisplayPhoneNumber, err)
						} else {
							h.notifyPhone(phone, businessphone.BannedNotice())
						}
					}
				}
				log.Printf("[phone-webhook] account_update: waba %s disabled, %d phones banned", wabaID, len(phones))
			}
		case "REINSTATE":
			if wabaID != "" {
				phones, err := h.repo.FindByWABAId(wabaID)
				if err != nil {
					log.Printf("[phone-webhook] account_update: failed to find phones for waba %s: %v", wabaID, err)
					return
				}
				var reinstated *businessphone.WhatsAppBusinessPhoneNumber
				for _, phone := range phones {
					if phone.Status == businessphone.StatusBanned || phone.Status == businessphone.StatusFlagged {
						phone.Status = businessphone.StatusConnected
						if err := h.repo.Update(phone.ID, phone); err != nil {
							log.Printf("[phone-webhook] account_update: failed to reinstate phone %s: %v", phone.DisplayPhoneNumber, err)
						} else {
							h.clearPhone(phone, businessphone.ReinstatedNotice().Ends...)
							if reinstated == nil {
								reinstated = phone
							}
						}
					}
				}
				log.Printf("[phone-webhook] account_update: waba %s reinstated, %d phones restored", wabaID, len(phones))
				if reinstated != nil {
					h.notifyPhone(reinstated, businessphone.ReinstatedNotice())
				}
			}
		}
	}
}

func (h *handlePhoneWebhook) handleAccountReviewUpdate(wabaID string, v businessphone.PhoneWebhookValue) {
	if wabaID == "" {
		log.Printf("[phone-webhook] account_review_update: missing waba_id")
		return
	}

	decision := strings.ToUpper(v.Decision)
	if decision == "" {
		log.Printf("[phone-webhook] account_review_update: missing decision for waba %s", wabaID)
		return
	}

	wabaEntity, err := h.wabaRepo.FindByMetaWABAId(wabaID)
	if err != nil {
		log.Printf("[phone-webhook] account_review_update: waba %s not found: %v", wabaID, err)
		return
	}

	if wabaEntity.AccountReviewStatus != decision {
		wabaEntity.AccountReviewStatus = decision
		if err := h.wabaRepo.Update(wabaEntity.ID, wabaEntity); err != nil {
			log.Printf("[phone-webhook] account_review_update: failed to update waba %s: %v", wabaID, err)
		} else {
			log.Printf("[phone-webhook] account_review_update: waba %s review decision=%s", wabaID, decision)
		}
	}
}

func (h *handlePhoneWebhook) handleBusinessStatusUpdate(wabaID string, v businessphone.PhoneWebhookValue) {
	if wabaID == "" {
		log.Printf("[phone-webhook] business_status_update: missing waba_id")
		return
	}

	event := strings.ToUpper(v.Event)
	log.Printf("[phone-webhook] business_status_update: waba=%s event=%s", wabaID, event)

	wabaEntity, err := h.wabaRepo.FindByMetaWABAId(wabaID)
	if err != nil {
		log.Printf("[phone-webhook] business_status_update: waba %s not found: %v", wabaID, err)
		return
	}

	changed := false

	switch event {
	case "BUSINESS_VERIFICATION_APPROVED", "VERIFIED":
		if wabaEntity.BusinessVerificationStatus != "VERIFIED" {
			wabaEntity.BusinessVerificationStatus = "VERIFIED"
			changed = true
		}
	case "BUSINESS_VERIFICATION_REJECTED", "REJECTED":
		if wabaEntity.BusinessVerificationStatus != "REJECTED" {
			wabaEntity.BusinessVerificationStatus = "REJECTED"
			changed = true
		}
	case "BUSINESS_VERIFICATION_REQUIRED", "PENDING":
		if wabaEntity.BusinessVerificationStatus != "PENDING" {
			wabaEntity.BusinessVerificationStatus = "PENDING"
			changed = true
		}
	default:

		if v.Decision != "" {
			status := strings.ToUpper(v.Decision)
			if wabaEntity.BusinessVerificationStatus != status {
				wabaEntity.BusinessVerificationStatus = status
				changed = true
			}
		}
	}

	if changed {
		if err := h.wabaRepo.Update(wabaEntity.ID, wabaEntity); err != nil {
			log.Printf("[phone-webhook] business_status_update: failed to update waba %s: %v", wabaID, err)
		} else {
			log.Printf("[phone-webhook] business_status_update: waba %s updated (verification=%s)", wabaID, wabaEntity.BusinessVerificationStatus)
		}
	}
}

func normalizePhoneNumber(raw string) string {
	if raw == "" {
		return ""
	}

	var b strings.Builder
	for _, c := range raw {
		if c == '+' || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func conversationLimitToTier(maxConversations int) string {
	switch {
	case maxConversations <= 0:
		return ""
	case maxConversations <= 250:
		return "TIER_250"
	case maxConversations <= 1000:
		return "TIER_1K"
	case maxConversations <= 10000:
		return "TIER_10K"
	case maxConversations <= 100000:
		return "TIER_100K"
	default:
		return "TIER_UNLIMITED"
	}
}
